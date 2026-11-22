package core

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 同一域名会解析出多个节点，其中可能只有个别可达。旧实现串行探测并限制
// 最多 4 个地址，一旦前面的地址全失败就会漏掉真正可用的节点。
func TestCDNProbeReachesAddressBeyondTheFirstFour(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("segment bytes"))
	}))
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	var dialed sync.Map
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed.Store(address, true)
		// 只有标记为 good 的地址能连上本地服务器。
		if address != "111.170.2.9:443" {
			return nil, errors.New("节点不可达")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}

	// 6 个不可达地址排在前面，可达的那个排在第 7 位。
	answers := []string{
		"111.170.2.1", "111.170.2.2", "111.170.2.3", "111.170.2.4",
		"111.170.2.5", "111.170.2.6", "111.170.2.9",
	}
	var body strings.Builder
	for index, address := range answers {
		if index > 0 {
			body.WriteString(",")
		}
		body.WriteString(`{"type":1,"TTL":120,"data":"` + address + `"}`)
	}
	resolver := newDNSResolver(base)
	resolver.client.Transport = sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		return sourceFixtureResponse(request, 200, `{"Status":0,"Answer":[`+body.String()+`]}`), nil
	})
	transport := newCDNTransport(base, resolver)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	response, err := client.Get("https://sjljsla.lkkwip.cn/segment.ts")
	if err != nil {
		t.Fatalf("reachable address beyond the first four was skipped: %v", err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(data) != "segment bytes" {
		t.Fatalf("unexpected body %q", data)
	}
	if _, ok := dialed.Load("111.170.2.9:443"); !ok {
		t.Fatal("the reachable address was never dialed")
	}
}

func TestCDNFailureReportsTriedAddresses(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, errors.New("节点不可达")
	}
	resolver := newDNSResolver(base)
	resolver.client.Transport = sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		return sourceFixtureResponse(request, 200,
			`{"Status":0,"Answer":[{"type":1,"TTL":120,"data":"111.170.2.1"},{"type":1,"TTL":120,"data":"111.170.2.2"}]}`), nil
	})
	transport := newCDNTransport(base, resolver)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	_, err := client.Get("https://sjljsla.lkkwip.cn/segment.ts")
	if err == nil {
		t.Fatal("unreachable media host reported success")
	}
	message := err.Error()
	if !strings.Contains(message, "111.170.2.1") || !strings.Contains(message, "111.170.2.2") {
		t.Fatalf("failure did not report the addresses it tried: %s", message)
	}
}
