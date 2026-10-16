package core

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeLiteEntryDSDUsesPublishedMirrorWhenLegacyEntryFails(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "www.dsd.com.se" {
			return nil, errors.New("TLS connection reset by peer")
		}
		if request.URL.Hostname() != "www.dsd901.com" {
			t.Fatalf("unexpected mirror: %s", request.URL.Hostname())
		}
		return sourceFixtureResponse(request, 200, `<a href="/index.php/vod/type/id/1.html">短剧</a>`), nil
	})
	d.cfg.DSDURL = "https://www.dsd.com.se"
	categories, err := d.fetchDSDCategories(context.Background(), true)
	if err != nil || len(categories) != 1 || categories[0].ID != "1" {
		t.Fatalf("legacy entry prevented catalog recovery: %v %+v", err, categories)
	}
	if d.providerHosts[sourceDSD] != "https://www.dsd901.com" {
		t.Fatal("working mirror was not remembered")
	}
	_, address, err := d.fetchProviderPage(context.Background(), "https://www.dsd.com.se/index.php/vod/type/id/1/page/2.html", "https://www.dsd.com.se/", dsdUserAgent)
	if err != nil || !strings.HasPrefix(address, "https://www.dsd901.com/") {
		t.Fatalf("later requests did not retain the recovered mirror: %s %v", address, err)
	}
}

func TestNativeLiteEntryDNSRecoveryPreservesHostAndTLSName(t *testing.T) {
	var ordinary, lookups atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != "www.dsd901.com" || request.TLS.ServerName != "www.dsd901.com" || request.URL.RequestURI() != "/index.php/vod/type/id/1/page/2.html?fixture=a%2Fb" {
			t.Errorf("recovery changed request identity: host=%s SNI=%s path=%s", request.Host, request.TLS.ServerName, request.URL.RequestURI())
		}
		_, _ = writer.Write([]byte("catalog text"))
	}))
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "www.dsd901.com:443" {
			ordinary.Add(1)
			return nil, errors.New("ordinary DNS route is stale")
		}
		if address != "93.184.216.34:443" {
			t.Fatalf("unexpected dial destination: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	resolver := newDNSResolver(base)
	resolver.client.Transport = sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		lookups.Add(1)
		if request.URL.Query().Get("name") != "www.dsd901.com" || request.URL.Query().Get("edns_client_subnet") != dnsAlternateSubnet {
			t.Fatal("entry recovery omitted the alternate subnet")
		}
		return sourceFixtureResponse(request, 200, `{"Status":0,"Answer":[{"type":1,"TTL":120,"data":"93.184.216.34"}]}`), nil
	})
	transport := newCDNTransport(base, resolver)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	for i := 0; i < 2; i++ {
		response, err := client.Get("https://www.dsd901.com/index.php/vod/type/id/1/page/2.html?fixture=a%2Fb")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != "catalog text" {
			t.Fatalf("entry failed after DNS recovery: %v", err)
		}
	}
	if lookups.Load() != 1 || ordinary.Load() != 0 {
		t.Fatalf("working entry route was not reused: lookups=%d ordinary=%d", lookups.Load(), ordinary.Load())
	}
}

func TestNativeLiteEntryYeguoDiscoversBusinessBaseFromImportedModule(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/":
			return sourceFixtureResponse(request, 200, `<script type="module" src="/_nuxt/entry.js"></script><script id="__NUXT_DATA__">[{"state":1},{}]</script>`), nil
		case "/_nuxt/entry.js":
			return sourceFixtureResponse(request, 200, `import {c} from "./codec.js";import {a} from "./api.js";`), nil
		case "/_nuxt/codec.js":
			return sourceFixtureResponse(request, 200, `const configuration={version:"v0",mode:"CBC",padding:"Pkcs7",key:"0123456789abcdef",iv:"abcdef0123456789",sign_key:"synthetic-signing-key"};`), nil
		case "/_nuxt/api.js":
			return sourceFixtureResponse(request, 200, "const origin=()=>`https://www.yeguodj.com/api.php`;const call=()=>request('/api/home/contentOptions',{baseURL:origin(),method:'GET'});"), nil
		default:
			return nil, errors.New("unexpected bootstrap request")
		}
	})
	access, err := d.yeguoClient().configuration(context.Background())
	if err != nil || access == nil || access.base != "https://www.yeguodj.com/api.php" || len(access.key) != 16 || len(access.iv) != 16 {
		t.Fatalf("current site format could not complete configuration: %v", err)
	}
}

func TestNativeLiteEntryYeguoRejectsUnrelatedScriptURLs(t *testing.T) {
	for _, script := range []string{
		"const origin=()=>`https://api.shuifeng.cc/api.php`;const call={baseURL:origin()};",
		"const origin=()=>`https://www.yeguodj.com.evil.test/api.php`;const call={baseURL:origin()};",
		"const origin=()=>`https://user:secret@www.yeguodj.com/api.php`;const call={baseURL:origin()};",
		"const origin=()=>`https://www.yeguodj.com/api.php?token=secret`;const call={baseURL:origin()};",
		"const origin=()=>`http://www.yeguodj.com/api.php`;const call={baseURL:origin()};",
		"const origin=()=>`https://www.yeguodj.com/api.php`;const unrelated={trackingURL:origin()};",
	} {
		if yeguoScriptAPIBase(script) != "" {
			t.Fatal("unrelated or unsafe URL was accepted as the business API")
		}
	}
	for _, address := range []string{"https://www.ygdj15.com/", "https://yeguodj.com/", "https://www.dsd901.com/", "https://www.dsd901.lol/"} {
		if providerSourceForURL(address) == "" {
			t.Fatalf("published entry is not recognized: %s", address)
		}
	}
	if !isYeguoActiveSite("https://yeguodj.com") || isYeguoActiveSite("https://yeguodj.com.evil.test") || sourceEntryDNSHost("www.dsd901.com.evil.test") {
		t.Fatal("source entry boundary is incorrect")
	}
}

func TestNativeLiteEntryDNSHonorsProxySelectionErrors(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	selectionError := errors.New("proxy selection denied")
	base.Proxy = func(*http.Request) (*url.URL, error) { return nil, selectionError }
	resolver := newDNSResolver(base)
	resolver.client.Transport = sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		t.Fatal("entry recovery bypassed failed proxy selection")
		return nil, nil
	})
	transport := newCDNTransport(base, resolver)
	defer transport.CloseIdleConnections()
	request, _ := http.NewRequest(http.MethodGet, "https://www.dsd901.com/", nil)
	_, err := transport.RoundTrip(request)
	if !errors.Is(err, selectionError) {
		t.Fatalf("lost proxy policy error: %v", err)
	}
}

func TestNativeLiteEntryDSDPlaybackRetainsRecoveredOrigin(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "www.dsd.com.se" {
			return nil, errors.New("old entry unavailable")
		}
		if request.URL.Hostname() != "www.dsd901.com" || !strings.HasPrefix(request.URL.Path, "/index.php/vod/play/") {
			t.Fatal("unexpected request while preparing playback")
		}
		return sourceFixtureResponse(request, 200, `<script>var player_fixture={"id":"123","sid":1,"nid":1,"url":"/video/fixture.mp4?sign=fixture","vod_data":{"vod_name":"合成剧集"}};</script>`), nil
	})
	d.cfg.DSDURL = "https://www.dsd.com.se"
	_, chapters, err := d.fetchDSDDetail(context.Background(), "123")
	if err != nil || len(chapters) != 1 {
		t.Fatalf("detail could not recover: %v", err)
	}
	if !strings.HasPrefix(chapters[0].PageURL, "https://www.dsd901.com/") || chapters[0].Referer != "https://www.dsd901.com/" {
		t.Fatal("recovered detail returned chapter URLs from the old entry")
	}
	media, err := d.resolveDSDMedia(context.Background(), Task{DramaID: "dsd:123", Chapter: chapters[0]})
	if err != nil || !strings.HasPrefix(media.URL, "https://www.dsd901.com/") || media.Referer != "https://www.dsd901.com/addons/vplayer/" {
		t.Fatalf("playback lost the working source origin: %v", err)
	}
}
