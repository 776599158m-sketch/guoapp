package core

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

type detailCancellationTransport struct {
	started chan struct{}
	stop    chan struct{}
	once    sync.Once
}

func (transport *detailCancellationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.once.Do(func() { close(transport.started) })
	select {
	case <-request.Context().Done():
		return nil, request.Context().Err()
	case <-transport.stop:
		return nil, context.Canceled
	}
}

func TestNativeLitePlaybackDetailCancellationPreservesPlayback(t *testing.T) {
	if buildLite != "true" || buildAllSources != "true" {
		t.Skip("requires Lite and all-source linker flags")
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	transport := &detailCancellationTransport{started: make(chan struct{}), stop: make(chan struct{})}
	defer close(transport.stop)
	engine.downloader.cfg.HuangguoAIURL = "https://detail.example.test"
	engine.downloader.client.Transport = transport
	nativeState.Lock()
	previous := nativeState.engine
	nativeState.engine = engine
	nativeState.Unlock()
	defer func() {
		nativeState.Lock()
		nativeState.engine = previous
		nativeState.Unlock()
	}()
	playback, finish, err := engine.nativeBeginPlayback(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	input := nativeInput{Action: "detail", Session: "fixture:detail-launch-1:", Sequence: 1,
		Drama: nativeDrama{ID: "huangguoai:fixture", Source: sourceHuangguoAI}}
	result := make(chan error, 1)
	go func() { _, err := nativeDispatch(input); result <- err }()
	select {
	case <-transport.started:
	case <-time.After(time.Second):
		t.Fatal("detail request did not start")
	}
	_, err = nativeDispatch(nativeInput{Action: "cancelRead", Session: input.Session, Sequence: input.Sequence})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("detail request was not canceled: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("leaving did not stop the detail request promptly")
	}
	if playback.Err() != nil {
		t.Fatal("detail cancellation stopped playback")
	}
	engine.readMu.Lock()
	remaining := engine.readCount
	engine.readMu.Unlock()
	if remaining != 0 {
		t.Fatalf("detail request leaked: %d", remaining)
	}
}
