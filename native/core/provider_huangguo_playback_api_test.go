package core

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const huangguoAIPlaylistFixture = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXTINF:6.000,\nsegment0.ts\n"

func huangguoAIMediaFixture(request *http.Request, allowMedia bool) (*http.Response, bool) {
	if strings.HasSuffix(strings.ToLower(request.URL.Path), ".m3u8") {
		if !allowMedia {
			return nil, false
		}
		return sourceFixtureResponse(request, 200, huangguoAIPlaylistFixture), true
	}
	return nil, false
}

func TestHuangguoAIPlaybackUsesEpisodeAPI(t *testing.T) {
	var playbackCalls int
	media := "https://media.example.test/fixture/ep-2/index.m3u8"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "/playback") {
			playbackCalls++
			if request.Method != http.MethodPost {
				t.Fatalf("playback API must use POST, got %s", request.Method)
			}
			if request.URL.Path != "/api/videos/fixture/episodes/2/playback" {
				t.Fatalf("unexpected playback resource: %s", request.URL.Path)
			}
			return sourceFixtureResponse(request, 200,
				`{"data":{"mode":1,"manifest_method":"GET","id":"fixture","ep":2,"streams":{"main":"`+media+`","hevc":"","preview":"`+media+`"}},"status":1}`), nil
		}
		if response, ok := huangguoAIMediaFixture(request, true); ok {
			return response, nil
		}
		return sourceFixtureResponse(request, 404, ""), nil
	})
	resolved, err := d.resolveHuangguoAIPlayback(context.Background(), "fixture", "2", "https://huangguoai.com/")
	if err != nil {
		t.Fatalf("episode API playback failed: %v", err)
	}
	if playbackCalls == 0 {
		t.Fatal("playback API was never requested")
	}
	if resolved.URL != media || !strings.HasPrefix(resolved.Playlist, "#EXTM3U") {
		t.Fatalf("episode API media was not used: %+v", resolved)
	}
}

func TestHuangguoAIPlaybackRejectsPreviewOnlyAndMismatch(t *testing.T) {
	for _, fixture := range []struct {
		name string
		body string
	}{
		{"preview-only", `{"data":{"id":"fixture","ep":2,"manifest_method":"GET","streams":{"main":"","hevc":"","preview":"https://media.example.test/preview.m3u8"}},"status":1}`},
		{"other-drama", `{"data":{"id":"other","ep":2,"manifest_method":"GET","streams":{"main":"https://media.example.test/other.m3u8"}},"status":1}`},
		{"other-episode", `{"data":{"id":"fixture","ep":1,"manifest_method":"GET","streams":{"main":"https://media.example.test/first.m3u8"}},"status":1}`},
		{"invalid-status", `{"data":[],"status":0,"msg":"播放参数无效"}`},
		{"unsupported-method", `{"data":{"id":"fixture","ep":2,"manifest_method":"POST","streams":{"main":"https://media.example.test/second.m3u8"}},"status":1}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
				if strings.Contains(request.URL.Path, "/playback") {
					return sourceFixtureResponse(request, 200, fixture.body), nil
				}
				if response, ok := huangguoAIMediaFixture(request, true); ok {
					return response, nil
				}
				return sourceFixtureResponse(request, 404, ""), nil
			})
			if media, err := d.resolveHuangguoAIPlayback(context.Background(), "fixture", "2", "https://huangguoai.com/"); err == nil {
				t.Fatalf("unusable payload produced media: %+v", media)
			}
		})
	}
}

func TestHuangguoAIPlaybackFallsBackToPageExtraction(t *testing.T) {
	var sawAPI, sawPage bool
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "/playback") {
			sawAPI = true
			return sourceFixtureResponse(request, 404, ""), nil
		}
		if request.URL.Path == "/video/fixture/ep-2/" {
			sawPage = true
			return sourceFixtureResponse(request, 200,
				`<script id="videoInitialData">{"id":"fixture","ep":2,"videoSrc":"https://media.example.test/second.m3u8"}</script>`), nil
		}
		if response, ok := huangguoAIMediaFixture(request, true); ok {
			return response, nil
		}
		return sourceFixtureResponse(request, 404, ""), nil
	})
	media, err := d.resolveProviderMedia(context.Background(), Task{
		DramaID: providerDramaID(sourceHuangguoAI, "fixture"), Index: 2,
		Chapter: Chapter{Source: sourceHuangguoAI, PageURL: "https://huangguoai.com/video/fixture/ep-2/", CurrentEpisode: rawEpisode(2)},
	})
	if err != nil {
		t.Fatalf("page fallback failed: %v", err)
	}
	if !sawAPI || !sawPage {
		t.Fatalf("expected API first then page fallback, api=%v page=%v", sawAPI, sawPage)
	}
	if media.URL != "https://media.example.test/second.m3u8" {
		t.Fatalf("page fallback media was not used: %+v", media)
	}
}
