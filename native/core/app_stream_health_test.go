package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func nativeStreamHealthFixtureEngine(t *testing.T, transport sourceFixtureTransport) *nativeEngine {
	t.Helper()
	return &nativeEngine{downloader: sourceFixtureDownloader(t, transport)}
}

func TestNativeStreamReopensAfterListenerStops(t *testing.T) {
	engine := nativeStreamHealthFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		t.Fatalf("loopback recovery requested upstream: %s", request.URL.Hostname())
		return nil, nil
	})
	media := providerMedia{URL: "https://media.example.test/index.m3u8", Playlist: "#EXTM3U\n#EXT-X-ENDLIST\n"}
	choice := nativePlaybackChoices(media, 0)
	first, err := engine.nativeOpenPlayback(context.Background(), choice)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	t.Cleanup(client.CloseIdleConnections)
	check := func(address string) error {
		response, err := client.Get(address)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "#EXTM3U") {
			t.Fatalf("invalid loopback playlist: status=%d body=%s", response.StatusCode, body)
		}
		return nil
	}
	if err := check(first.URL); err != nil {
		t.Fatal(err)
	}
	stopped := engine.stream
	if err := stopped.server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := check(first.URL); err == nil {
		t.Fatal("stopped listener was still reachable")
	}
	second, err := engine.nativeOpenPlayback(context.Background(), choice)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.stream.server.Close() })
	if err := check(second.URL); err != nil {
		t.Fatalf("重新解析后本机播放服务仍拒绝连接: %v", err)
	}
	if engine.stream == stopped {
		t.Fatal("playback reused the stopped server")
	}
	if !second.LocalReady || !second.LocalRestarted {
		t.Fatal("recovered listener was not confirmed in playback plan")
	}
}

func TestNativeStreamHealthBypassesProxyAndPreservesSessions(t *testing.T) {
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		proxyCalls.Add(1)
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	engine := nativeStreamHealthFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		t.Fatal("health check requested upstream media")
		return nil, nil
	})
	choice := nativePlaybackChoices(providerMedia{URL: "https://media.example.test/index.m3u8", Playlist: "#EXTM3U\n#EXT-X-ENDLIST\n"}, 0)
	first, err := engine.nativeOpenPlayback(context.Background(), choice)
	if err != nil || !first.LocalReady || first.LocalRestarted {
		t.Fatalf("initial local service was not confirmed: %+v %v", first, err)
	}
	stream := engine.stream
	defer stream.nativeClose()
	second, err := engine.nativeOpenPlayback(context.Background(), choice)
	if err != nil || !second.LocalReady || second.LocalRestarted || engine.stream != stream || proxyCalls.Load() != 0 {
		t.Fatalf("healthy local service was replaced or used proxy: %+v %v", second, err)
	}
	stream.mu.Lock()
	count := len(stream.sessions)
	stream.mu.Unlock()
	if count != 2 {
		t.Fatal("health checks removed active playback sessions")
	}
}

func TestNativeStreamCancelledHealthCheckPreservesCurrentServer(t *testing.T) {
	engine := nativeStreamHealthFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		t.Fatal("unexpected upstream request")
		return nil, nil
	})
	choice := nativePlaybackChoices(providerMedia{URL: "https://media.example.test/index.m3u8", Playlist: "#EXTM3U\n#EXT-X-ENDLIST\n"}, 0)
	if _, err := engine.nativeOpenPlayback(context.Background(), choice); err != nil {
		t.Fatal(err)
	}
	stream := engine.stream
	defer stream.nativeClose()
	hanging := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer hanging.Close()
	address := stream.address
	stream.address = hanging.URL
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := engine.nativeOpenPlayback(ctx, choice)
	stream.address = address
	if err != context.DeadlineExceeded || engine.stream != stream || !stream.nativeHealthy(context.Background()) {
		t.Fatalf("canceled health check destroyed the current listener: %v", err)
	}
}

func TestNativeStreamConcurrentRecoverySharesOneListener(t *testing.T) {
	engine := nativeStreamHealthFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		t.Fatal("recovery requested upstream")
		return nil, nil
	})
	choice := nativePlaybackChoices(providerMedia{URL: "https://media.example.test/index.m3u8", Playlist: "#EXTM3U\n#EXT-X-ENDLIST\n"}, 0)
	if _, err := engine.nativeOpenPlayback(context.Background(), choice); err != nil {
		t.Fatal(err)
	}
	if err := engine.stream.server.Close(); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	plans := make(chan nativePlan, 4)
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			plan, err := engine.nativeOpenPlayback(context.Background(), choice)
			plans <- plan
			failures <- err
		}()
	}
	workers.Wait()
	close(plans)
	close(failures)
	defer engine.stream.nativeClose()
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	restarts := 0
	for plan := range plans {
		if !plan.LocalReady || !strings.HasPrefix(plan.URL, engine.stream.address+"/") {
			t.Fatal("concurrent recovery returned an obsolete listener")
		}
		if plan.LocalRestarted {
			restarts++
		}
	}
	if restarts != 1 {
		t.Fatalf("recovery rebuilt %d listeners", restarts)
	}
}

func TestNativeStreamHealthRejectsRedirectAndWrongInstance(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirected.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer other.Close()
	for _, redirect := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if redirect {
				http.Redirect(writer, request, other.URL, http.StatusFound)
				return
			}
			writer.Header().Set("X-Stream-Instance", "wrong-instance")
			writer.WriteHeader(http.StatusNoContent)
		}))
		stream := &nativeStreamServer{address: server.URL, healthID: "expected-instance", stopped: make(chan struct{})}
		if stream.nativeHealthy(context.Background()) {
			t.Fatal("health check accepted another service")
		}
		server.Close()
	}
	if redirected.Load() != 0 {
		t.Fatal("local health probe followed redirect")
	}
}
