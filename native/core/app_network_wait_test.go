package core

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestNativeLiteNetworkRequestBudgets(t *testing.T) {
	for _, item := range []struct {
		name  string
		input nativeInput
		want  time.Duration
	}{
		{"huangguo ranking", nativeInput{Action: "rankings", Board: "huangguo-hot"}, 15 * time.Second},
		{"hongguo ranking", nativeInput{Action: "rankings", Board: "hongguo-hot"}, time.Minute},
		{"huangguo detail", nativeInput{Action: "detail", Session: "foreground", Drama: nativeDrama{ID: "huangguoai:fixture"}}, 30 * time.Second},
		{"huangguo background detail", nativeInput{Action: "detail", Drama: nativeDrama{ID: "huangguoai:fixture"}}, time.Minute},
		{"huangguo resolve", nativeInput{Action: "resolve", Drama: nativeDrama{ID: "huangguoai:fixture"}}, 30 * time.Second},
		{"legacy resolve", nativeInput{Action: "resolve", Drama: nativeDrama{ID: "cloudfront:fixture"}}, 30 * time.Second},
		{"video resolve", nativeInput{Action: "resolve", Drama: nativeDrama{ID: "huangguo-video:fixture"}}, 30 * time.Second},
		{"other detail", nativeInput{Action: "detail", Session: "foreground", Drama: nativeDrama{ID: "huangdou:fixture"}}, time.Minute},
		{"preload", nativeInput{Action: "preload"}, 15 * time.Second},
		{"danmaku", nativeInput{Action: "danmaku"}, 10 * time.Second},
		{"move", nativeInput{Action: "moveDownloads"}, 10 * time.Minute},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := nativeRequestTimeout(item.input); got != item.want {
				t.Fatalf("timeout=%s want=%s", got, item.want)
			}
		})
	}
}

func TestNativeLiteNetworkReadCancelsUpstream(t *testing.T) {
	if buildLite != "true" || buildAllSources != "true" {
		t.Skip("requires Lite/all-source build")
	}
	for _, action := range []string{"rankings", "detail"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan struct{}, 1)
			engine := &nativeEngine{downloader: sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
				select {
				case started <- struct{}{}:
				default:
				}
				<-request.Context().Done()
				return nil, request.Context().Err()
			})}
			nativeState.Lock()
			previous := nativeState.engine
			nativeState.engine = engine
			nativeState.Unlock()
			defer func() {
				nativeState.Lock()
				nativeState.engine = previous
				nativeState.Unlock()
			}()
			input := nativeInput{Action: action, Session: "network-test:" + action, Sequence: 1,
				Board: "huangguo-hot", Page: 1, Drama: nativeDrama{ID: "huangguoai:fixture", Source: sourceHuangguoAI}}
			for _, cancelRead := range []bool{true, false} {
				input.Sequence++
				done := make(chan error, 1)
				go func() {
					_, err := nativeDispatchWithTimeout(input, 250*time.Millisecond)
					done <- err
				}()
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("dispatch did not reach upstream")
				}
				want := context.DeadlineExceeded
				if cancelRead {
					want = context.Canceled
					if _, err := nativeDispatch(nativeInput{Action: "cancelRead", Session: input.Session, Sequence: input.Sequence}); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case err := <-done:
					if !errors.Is(err, want) {
						t.Fatalf("dispatch error=%v want=%v", err, want)
					}
				case <-time.After(time.Second):
					t.Fatal("upstream request ignored cancellation or deadline")
				}
				engine.readMu.Lock()
				count := engine.readCount
				engine.readMu.Unlock()
				if count != 0 {
					t.Fatal("completed request retained a read slot")
				}
			}
		})
	}
}
