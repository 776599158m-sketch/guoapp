package core

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func TestNativeStreamSegmentsAllowCachingAndTyped(t *testing.T) {
	segment := bytes.Repeat([]byte{0x47, 0x40, 0x11, 0x10}, 64)
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST\n"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/play.m3u8" {
			return sourceFixtureResponse(request, 200, playlist), nil
		}
		if request.URL.Path != "/segment.ts" {
			t.Fatalf("unexpected upstream request: %s", request.URL.Path)
		}
		response := sourceFixtureResponse(request, 200, string(segment))
		response.Header.Set("Content-Type", "binary/octet-stream")
		return response, nil
	})
	stream, err := newNativeStreamServer(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stream.server.Close() })
	address, token := stream.nativeOpen(providerMedia{URL: "https://media.example.test/play.m3u8"})
	t.Cleanup(func() { stream.nativeRelease(token) })

	head := func(address string) *http.Response {
		t.Helper()
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		return response
	}

	manifest := head(address)
	if manifest.StatusCode != 200 {
		t.Fatalf("playlist status %d", manifest.StatusCode)
	}
	if cache := manifest.Header.Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("playlist must stay uncached, got %q", cache)
	}
	body := func() string {
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return string(data)
	}()
	segmentURL := ""
	for _, line := range bytes.Split([]byte(body), []byte("\n")) {
		text := string(bytes.TrimSpace(line))
		if text != "" && text[0] != '#' {
			segmentURL = text
			break
		}
	}
	if segmentURL == "" {
		t.Fatalf("no segment in rewritten playlist: %s", body)
	}
	media := head(segmentURL)
	if media.StatusCode != 200 {
		t.Fatalf("segment status %d", media.StatusCode)
	}
	if got := media.Header.Get("Content-Type"); got != "video/mp2t" {
		t.Fatalf("segment content type must be video/mp2t, got %q", got)
	}
	cache := media.Header.Get("Cache-Control")
	if cache == "" || cache == "no-store" {
		t.Fatalf("segment must be cacheable, got %q", cache)
	}
}
