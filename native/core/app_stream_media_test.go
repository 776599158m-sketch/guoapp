package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestNativeStreamEncryptedHLSKeepsSegmentsBinary(t *testing.T) {
	key := bytes.Repeat([]byte{0x12}, 16)
	segment := bytes.Repeat([]byte{0xa5, 0xb6, 0xc7, 0xd8}, 64)
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/play.m3u8" {
			return sourceFixtureResponse(request, 200, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.key\"\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST\n"), nil
		}
		if request.URL.Path != "/segment.ts" {
			t.Fatalf("unexpected upstream request: %s", request.URL.Path)
		}
		response := sourceFixtureResponse(request, 200, string(segment))
		response.Header.Set("Content-Type", "video/mp2t")
		return response, nil
	})
	stream, err := newNativeStreamServer(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stream.server.Close() })
	address, token := stream.nativeOpen(providerMedia{URL: "https://media.example.test/play.m3u8", HLSKey: key})
	t.Cleanup(func() { stream.nativeRelease(token) })
	read := func(address string) []byte {
		t.Helper()
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("proxy returned HTTP %d: %s, %v", response.StatusCode, body, err)
		}
		return body
	}
	playlist := string(read(address))
	keyMatch := nativePlaylistURI.FindStringSubmatch(playlist)
	if len(keyMatch) != 2 || !bytes.Equal(read(keyMatch[1]), key) {
		t.Fatal("proxy lost the source-provided HLS key")
	}
	var segmentURL string
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasPrefix(line, "http://") {
			segmentURL = line
		}
	}
	if segmentURL == "" || !bytes.Equal(read(segmentURL), segment) {
		t.Fatal("encrypted video bytes were treated as a playlist or changed")
	}
}

func TestNativeStreamMP4QueryDoesNotChangeMediaType(t *testing.T) {
	for _, query := range []string{"?format_hint=hls", "?signature=m3u8-value"} {
		t.Run(query, func(t *testing.T) {
			media := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{0x11}, 64)...)
			d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != "/video.mp4" || "?"+request.URL.RawQuery != query {
					t.Fatalf("media URL changed: %s", request.URL)
				}
				response := sourceFixtureResponse(request, 200, string(media))
				response.Header.Set("Content-Type", "video/mp4")
				return response, nil
			})
			stream, err := newNativeStreamServer(d)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stream.server.Close() })
			address, token := stream.nativeOpen(providerMedia{URL: "https://media.example.test/video.mp4" + query})
			t.Cleanup(func() { stream.nativeRelease(token) })
			response, err := http.Get(address)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "video/mp4" || !bytes.Equal(body, media) {
				t.Fatalf("binary MP4 was classified as a playlist: HTTP %d, %s, %v", response.StatusCode, body, err)
			}
		})
	}
}

func TestNativeStreamDetectsExtensionlessPlaylist(t *testing.T) {
	segment := bytes.Repeat([]byte{0x47, 0x12, 0x34, 0x56}, 188)
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/manifest" {
			response := sourceFixtureResponse(request, 200, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nsegment.ts?signature=m3u8-value\n#EXT-X-ENDLIST\n")
			response.Header.Set("Content-Type", "application/octet-stream")
			return response, nil
		}
		if request.URL.Path != "/segment.ts" {
			t.Errorf("unexpected request: %s", request.URL.Path)
		}
		response := sourceFixtureResponse(request, 200, string(segment))
		response.Header.Set("Content-Type", "video/mp2t")
		return response, nil
	})
	stream, err := newNativeStreamServer(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stream.server.Close() })
	address, token := stream.nativeOpen(providerMedia{URL: "https://media.example.test/manifest"})
	t.Cleanup(func() { stream.nativeRelease(token) })
	response, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("extensionless playlist not detected: HTTP %d, %v", response.StatusCode, err)
	}
	var segmentURL string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "http://") {
			segmentURL = line
		}
	}
	response, err = http.Get(segmentURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err = io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || !bytes.Equal(body, segment) {
		t.Fatalf("segment query changed its type: HTTP %d, %v", response.StatusCode, err)
	}
}

func TestNativeStreamMP4PreservesRangeAndHead(t *testing.T) {
	media := bytes.Repeat([]byte{0x11, 0x22, 0x33, 0x44}, 1024)
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodHead {
			response := sourceFixtureResponse(request, 200, "")
			response.Header.Set("Content-Type", "video/mp4")
			response.Header.Set("Content-Length", strconv.Itoa(len(media)))
			response.Header.Set("Accept-Ranges", "bytes")
			return response, nil
		}
		if request.Header.Get("Range") != "bytes=512-1023" {
			t.Errorf("range lost: %q", request.Header.Get("Range"))
		}
		response := sourceFixtureResponse(request, 206, string(media[512:1024]))
		response.Header.Set("Content-Type", "video/mp4")
		response.Header.Set("Content-Range", "bytes 512-1023/4096")
		response.Header.Set("Content-Length", "512")
		return response, nil
	})
	stream, err := newNativeStreamServer(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stream.server.Close() })
	address, token := stream.nativeOpen(providerMedia{URL: "https://media.example.test/video.mp4?format_hint=hls"})
	t.Cleanup(func() { stream.nativeRelease(token) })
	request, _ := http.NewRequest(http.MethodGet, address, nil)
	request.Header.Set("Range", "bytes=512-1023")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 206 || response.Header.Get("Content-Range") != "bytes 512-1023/4096" || !bytes.Equal(body, media[512:1024]) {
		t.Fatalf("range response changed: HTTP %d, %v", response.StatusCode, err)
	}
	response, err = http.Head(address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err = io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || response.ContentLength != int64(len(media)) || response.Header.Get("Content-Type") != "video/mp4" || len(body) != 0 {
		t.Fatalf("HEAD response changed: HTTP %d, length=%d, %v", response.StatusCode, response.ContentLength, err)
	}
}

func TestNativeStreamReportsUpstreamFailure(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		return sourceFixtureResponse(request, http.StatusForbidden, "denied"), nil
	})
	stream, err := newNativeStreamServer(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stream.server.Close() })
	address, token := stream.nativeOpen(providerMedia{URL: "https://media.example.test/video.mp4?token=secret"})
	t.Cleanup(func() { stream.nativeRelease(token) })
	diagnostics := stream.address + "/" + token + "/_diagnostics"
	response, err := http.Get(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("healthy session has a failure: HTTP %d", response.StatusCode)
	}
	response, err = http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("lost upstream status: HTTP %d", response.StatusCode)
	}
	response, err = http.Get(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	var failure nativeStreamFailure
	if err != nil || response.StatusCode != 200 || json.Unmarshal(body, &failure) != nil {
		t.Fatalf("cannot read proxy failure: HTTP %d, %v", response.StatusCode, err)
	}
	if failure.HTTPStatus != 403 || failure.Host != "media.example.test" || failure.Stage != "媒体响应" || strings.Contains(string(body), "secret") {
		t.Fatalf("failure is incomplete or includes credentials: %#v", failure)
	}
}
