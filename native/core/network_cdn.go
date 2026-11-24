package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type cdnRoute struct {
	address string
	expires time.Time
}

type cdnTransport struct {
	base       *http.Transport
	resolver   *dnsResolver
	mu         sync.Mutex
	transports map[string]*http.Transport
	preferred  map[string]cdnRoute
}

func newCDNTransport(base *http.Transport, resolver *dnsResolver) *cdnTransport {
	return &cdnTransport{base: base, resolver: resolver, transports: map[string]*http.Transport{}, preferred: map[string]cdnRoute{}}
}

func (transport *cdnTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	entry := sourceEntryDNSHost(request.URL.Hostname())
	if request.URL.Scheme != "https" || (!protectedCDNHost(request.URL.Hostname()) && !entry && request.Context().Value(nativeCoverNetworkKey{}) != true) || (request.Method != http.MethodGet && request.Method != http.MethodHead) {
		return transport.base.RoundTrip(request)
	}
	var proxyURL *url.URL
	var err error
	if transport.base.Proxy != nil {
		proxyURL, err = transport.base.Proxy(request)
		if err != nil {
			return nil, err
		}
	}
	routeKey := request.URL.Host
	if proxyURL != nil {
		routeKey += "|" + proxyURL.String()
	}
	transport.mu.Lock()
	preferred := transport.preferred[routeKey]
	transport.mu.Unlock()
	tried := map[string]bool{}
	var response *http.Response
	if preferred.address != "" && time.Now().Before(preferred.expires) {
		tried[preferred.address] = true
		response, err = transport.roundTripAddress(request, proxyURL, routeKey, preferred.address)
		if err == nil {
			return response, nil
		}
	} else if !entry || proxyURL != nil {
		response, err = transport.base.RoundTrip(request)
		if err == nil {
			return response, nil
		}
	}
	if request.Context().Err() != nil {
		return nil, request.Context().Err()
	}
	lastErr := err
	// 部分媒体域名用 GeoDNS：带国外 ECS 会返回境外节点，而这些节点在境内
	// TLS 握手直接被重置（实测 sjljsla.lkkwip.cn 的境外地址即如此）。媒体域名
	// 先按不带 ECS 的本地线路解析，再退国外 ECS；其余入口保持原顺序。
	subnets := []string{dnsAlternateSubnet, ""}
	if mediaCDNHost(request.URL.Hostname()) {
		subnets = []string{"", dnsAlternateSubnet}
	}
	for _, subnet := range subnets {
		entry, lookupErr := transport.resolver.lookupEntry(request.Context(), request.URL.Hostname(), subnet)
		if lookupErr != nil {
			lastErr = lookupErr
			if request.Context().Err() != nil {
				return nil, request.Context().Err()
			}
			continue
		}
		var fresh []string
		for _, address := range entry.addresses {
			if !tried[address] {
				fresh = append(fresh, address)
			}
		}
		// 同一域名的多个节点里可能只有部分可达（实测国内四个全通、境外两个
		// 握手即失败），因此并发探测而不是串行；串行会在前几个失败地址上
		// 耗尽配额，把真正可用的节点挤掉。
		if response, address, probeErr := transport.probeAddresses(request, proxyURL, routeKey, fresh); address != "" {
			for _, candidate := range fresh {
				tried[candidate] = true
			}
			if response != nil {
				transport.mu.Lock()
				transport.preferred[routeKey] = cdnRoute{address: address, expires: entry.expires}
				transport.mu.Unlock()
				return response, nil
			}
			lastErr = probeErr
		}
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
	}
	if entry && request.Context().Err() == nil {
		response, err = transport.base.RoundTrip(request)
		if err == nil {
			return response, nil
		}
		lastErr = err
	}
	// 日志里必须能看出到底尝试了哪些地址：同一域名按线路会解析到完全不同的
	// 节点，只有回执地址才能判断是线路问题还是节点问题。
	triedText := "无可用解析"
	if len(tried) > 0 {
		addresses := make([]string, 0, len(tried))
		for address := range tried {
			addresses = append(addresses, address)
		}
		sort.Strings(addresses)
		triedText = strings.Join(addresses, ",")
	}
	return nil, fmt.Errorf("资源 %s 连接失败（已尝试地址 %s）: %w", request.URL.Hostname(), triedText, publicError(lastErr))
}

// probeAddresses 并发尝试一组地址，返回第一个成功响应及其地址。全部失败时
// 返回最后一条错误；没有可试地址时返回空地址。
func (transport *cdnTransport) probeAddresses(request *http.Request, proxyURL *url.URL, routeKey string, addresses []string) (*http.Response, string, error) {
	if len(addresses) == 0 {
		return nil, "", nil
	}
	type probeResult struct {
		response *http.Response
		address  string
		err      error
	}
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	results := make(chan probeResult, len(addresses))
	var group sync.WaitGroup
	for _, address := range addresses {
		group.Add(1)
		go func(address string) {
			defer group.Done()
			response, err := transport.roundTripAddress(request.WithContext(ctx), proxyURL, routeKey, address)
			if err == nil {
				results <- probeResult{response: response, address: address}
				return
			}
			results <- probeResult{address: address, err: err}
		}(address)
	}
	go func() {
		group.Wait()
		close(results)
	}()
	var lastErr error
	var won probeResult
	wonAddress := ""
	for result := range results {
		if result.err == nil && wonAddress == "" {
			won = result
			wonAddress = result.address
			cancel()
			continue
		}
		if result.err != nil {
			lastErr = result.err
		}
		if result.response != nil && result.response.Body != nil && result.address != wonAddress {
			result.response.Body.Close()
		}
	}
	if wonAddress != "" {
		return won.response, wonAddress, nil
	}
	return nil, addresses[0], lastErr
}

func (transport *cdnTransport) roundTripAddress(request *http.Request, proxyURL *url.URL, routeKey, address string) (*http.Response, error) {
	pinned := transport.pinnedTransport(request.URL.Hostname(), proxyURL, routeKey, address)
	upstream := request.Clone(request.Context())
	upstream.Host = request.Host
	if upstream.Host == "" {
		upstream.Host = request.URL.Host
	}
	upstream.URL.Host = address
	if port := request.URL.Port(); port != "" {
		upstream.URL.Host = net.JoinHostPort(address, port)
	}
	response, err := pinned.RoundTrip(upstream)
	if response != nil {
		response.Request = request
	}
	return response, err
}

func (transport *cdnTransport) pinnedTransport(host string, proxyURL *url.URL, routeKey, address string) *http.Transport {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	key := routeKey + "|" + address
	if existing := transport.transports[key]; existing != nil {
		return existing
	}
	pinned := transport.base.Clone()
	if pinned.TLSClientConfig == nil {
		pinned.TLSClientConfig = &tls.Config{}
	}
	pinned.TLSClientConfig.ServerName = host
	pinned.Proxy = http.ProxyURL(proxyURL)
	if proxyURL != nil && proxyURL.Scheme == "https" {
		plainProxy := *proxyURL
		plainProxy.Scheme = "http"
		if plainProxy.Port() == "" {
			plainProxy.Host = net.JoinHostPort(proxyURL.Hostname(), "443")
		}
		pinned.Proxy = http.ProxyURL(&plainProxy)
		pinned.DialContext = transport.tlsProxyDialer(proxyURL.Hostname())
	}
	if len(transport.transports) >= 32 {
		for expiredKey, expiredTransport := range transport.transports {
			expiredTransport.CloseIdleConnections()
			delete(transport.transports, expiredKey)
			break
		}
	}
	transport.transports[key] = pinned
	return pinned
}

func (transport *cdnTransport) tlsProxyDialer(host string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		dial := transport.base.DialContext
		if dial == nil {
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			dial = dialer.DialContext
		}
		connection, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		config := &tls.Config{}
		if transport.base.TLSClientConfig != nil {
			config = transport.base.TLSClientConfig.Clone()
		}
		config.ServerName = host
		config.NextProtos = []string{"http/1.1"}
		secured := tls.Client(connection, config)
		if transport.base.TLSHandshakeTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, transport.base.TLSHandshakeTimeout)
			defer cancel()
		}
		if err := secured.HandshakeContext(ctx); err != nil {
			connection.Close()
			return nil, err
		}
		return secured, nil
	}
}

func (transport *cdnTransport) CloseIdleConnections() {
	transport.base.CloseIdleConnections()
	transport.resolver.client.CloseIdleConnections()
	transport.mu.Lock()
	defer transport.mu.Unlock()
	for key, pinned := range transport.transports {
		pinned.CloseIdleConnections()
		delete(transport.transports, key)
	}
	transport.preferred = map[string]cdnRoute{}
}
