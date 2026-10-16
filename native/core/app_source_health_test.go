package core

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNativeLiteSourceHealthDSDTimeoutStopsBeforePlayback(t *testing.T) {
	if buildAllSources != "true" {
		t.Skip("requires all-source build")
	}
	requests := []string{}
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.Path)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	engine := &nativeEngine{downloader: d, directory: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	err := engine.checkSource(ctx, sourceDSD, nativeDrama{}, true)
	report := engine.sourceStatus(sourceDSD).Health
	if !errors.Is(err, context.DeadlineExceeded) || report == nil || len(report.Steps) != 1 || report.Steps[0].Name != "入口与目录" {
		t.Fatalf("unexpected failure stage: error=%v report=%+v", err, report)
	}
	for _, path := range requests {
		if path != "/" {
			t.Fatalf("entry failure reached a later playback route: %s", path)
		}
	}
	t.Logf("stage=%s status=%s requests=%d playbackReached=false", report.Steps[0].Name, report.State, len(requests))
}

func TestNativeLiteSourceHealthYeguoResetDoesNotInheritEarlierHTTP200(t *testing.T) {
	if buildAllSources != "true" {
		t.Skip("requires all-source build")
	}
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "ygdj7.com":
			return sourceFixtureResponse(request, 200, `https://abandon.fzchosdi.cc https://able.fzchosdi.cc`), nil
		case "abandon.fzchosdi.cc":
			return sourceFixtureResponse(request, 200, `<html><body>unconfigured entry</body></html>`), nil
		default:
			return nil, errors.New("read: connection reset by peer")
		}
	})
	engine := &nativeEngine{downloader: d, directory: t.TempDir()}
	err := engine.checkSource(context.Background(), sourceYeguo, nativeDrama{}, true)
	report := engine.sourceStatus(sourceYeguo).Health
	if err == nil || !strings.Contains(err.Error(), "connection reset by peer") || report == nil || len(report.Steps) != 1 {
		t.Fatalf("unexpected outcome: error=%v report=%+v", err, report)
	}
	step := report.Steps[0]
	t.Logf("stage=%s state=%s recordedHost=%s recordedHTTP=%d resetError=true playbackReached=false", step.Name, step.State, step.Host, step.HTTPStatus)
	if step.HTTPStatus != 0 || step.Host != "able.fzchosdi.cc" {
		t.Fatal("final transport failure inherited another host's successful HTTP response")
	}
}

func TestNativeLiteSourceHealthKeepsActualHTTPResponse(t *testing.T) {
	if buildAllSources != "true" {
		t.Skip("requires all-source build")
	}
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
				response := sourceFixtureResponse(request, status, `<html><body>no catalog here</body></html>`)
				response.Header.Set("Cf-Ray", "fixture-ray")
				return response, nil
			})
			engine := &nativeEngine{downloader: d, directory: t.TempDir()}
			err := engine.checkSource(context.Background(), sourceDSD, nativeDrama{}, true)
			report := engine.sourceStatus(sourceDSD).Health
			if err == nil || report == nil || len(report.Steps) != 1 {
				t.Fatalf("unexpected result: %v %+v", err, report)
			}
			step := report.Steps[0]
			if step.State != "failed" || step.HTTPStatus != status || step.Host != "www.dsd.com.se" || step.CFRay != "fixture-ray" {
				t.Fatalf("lost actual HTTP response on validation failure: %+v", step)
			}
		})
	}
}
