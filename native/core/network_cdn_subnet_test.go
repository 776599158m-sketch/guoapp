package core

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 黄果旧版的分片全部由 lkkwip.cn 下发，该域名用 GeoDNS 分区：带国外 ECS 会返回
// 境外节点，而境外节点在境内 TLS 握手即失败。媒体域名必须先走本地线路解析。
func TestMediaCDNUsesLocalSubnetBeforeAlternate(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, errors.New("ordinary DNS route is stale")
	}

	for _, fixture := range []struct {
		name      string
		host      string
		wantFirst string
	}{
		{"media host prefers local subnet", "sjljsla.lkkwip.cn", ""},
		{"entry host keeps alternate first", "www.dsd901.com", dnsAlternateSubnet},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var order []string
			resolver := newDNSResolver(base)
			resolver.client.Transport = sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
				order = append(order, request.URL.Query().Get("edns_client_subnet"))
				return sourceFixtureResponse(request, 200, `{"Status":0,"Answer":[{"type":1,"TTL":120,"data":"93.184.216.34"}]}`), nil
			})
			transport := newCDNTransport(base, resolver)
			defer transport.CloseIdleConnections()
			// 只关心解析顺序：走一次 RoundTrip，境外/境内地址都不可达也无所谓。
			request, err := http.NewRequest(http.MethodGet, "https://"+fixture.host+"/segment.ts", nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = transport.RoundTrip(request)
			if len(order) == 0 || order[0] != fixture.wantFirst {
				firstSubnet := "<none>"
				if len(order) > 0 {
					firstSubnet = order[0]
				}
				t.Fatalf("first lookup subnet = %q, want %q (order %v)", firstSubnet, fixture.wantFirst, order)
			}
		})
	}
}

var _ = httptest.NewServer
