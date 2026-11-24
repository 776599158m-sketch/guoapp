package core

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestLegacyAPIProbeAllowsSlowHealthyEndpoint(t *testing.T) {
	for _, background := range []bool{false, true} {
		name := "foreground"
		if background {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != "/api/app/ping/check" {
					t.Errorf("unexpected probe: %s", request.URL.Path)
				}
				timer := time.NewTimer(5 * time.Second)
				defer timer.Stop()
				select {
				case <-timer.C:
					return sourceFixtureResponse(request, 200, `{"code":200}`), nil
				case <-request.Context().Done():
					return nil, request.Context().Err()
				}
			})
			ctx := context.Background()
			if background {
				ctx = context.WithValue(ctx, backgroundCatalogKey{}, true)
			}
			if err := d.probeAPIHost(ctx, "https://api.example.test"); err != nil {
				t.Fatalf("slow but healthy API was rejected: %v", err)
			}
		})
	}
}
