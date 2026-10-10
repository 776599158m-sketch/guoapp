package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestHuangguoAIEpisodesExcludeSharedRecommendations(t *testing.T) {
	var page strings.Builder
	for index := 1; index <= 6; index++ {
		fmt.Fprintf(&page, `<a class="hg-search-suggest__hot-title" href="/video/hot-%d/">Recommended fixture</a>`, index)
	}
	page.WriteString(`<a href="/video/fixture/" title="Fixture complete series">Fixture</a>
		<a href="/video/fixture/ep-2/">Next</a>
		<a class="hg-web-play__ep" href="/video/fixture/">1</a>
		<a class="hg-web-play__ep" href="/video/fixture/ep-2/">2</a>
		<a class="hg-web-play__ep" href="/video/fixture/ep-3/">3</a>
		<a href="/video/other/ep-4/">Episode 4</a>
		<a href="https://unrelated.example.test/video/fixture/ep-4/">4</a>
		<a href="/video/fixture/ep-0/">0</a>
		<a href="/video/fixture/ep-10001/">10001</a>`)
	episodes := parseHuangguoAIEpisodes(page.String(), "https://huangguoai.com/detail/fixture/", "fixture")
	if len(episodes) != 3 {
		t.Fatalf("expected only the three selected-drama episodes, got %d: %+v", len(episodes), episodes)
	}
	for index, episode := range episodes {
		if episode.Index != index+1 || !strings.HasPrefix(episode.URL, "https://huangguoai.com/video/fixture/") {
			t.Fatalf("incorrect episode identity at %d: %+v", index, episode)
		}
	}
}

func TestHuangguoAIEpisodesUseAddressNumbersAndDeduplicate(t *testing.T) {
	page := `<a href="/video/fixture/ep-12/">Episode 1</a>
		<a href="/video/fixture/ep-1/">1</a>
		<a href="/video/fixture/">Fixture</a>
		<a href="/video/fixture/ep-2/?tracking=one">Next</a>
		<a href="/video/fixture/ep-2/?tracking=two">2</a>`
	episodes := parseHuangguoAIEpisodes(page, "https://huangguoai.com/detail/fixture/", "fixture")
	if len(episodes) != 3 {
		t.Fatalf("duplicate entries changed the selected-drama episode count: %+v", episodes)
	}
	for index, number := range []int{1, 2, 12} {
		if episodes[index].Index != number {
			t.Fatalf("address number %d was replaced by link text or list position: %+v", number, episodes)
		}
	}
}

func TestHuangguoAIPlaybackSelectsRequestedEpisode(t *testing.T) {
	page := `<script id="videoInitialData" type="application/json">{"id":"fixture","ep":1,"videoSrc":"https://media.example.test/first.mp4","epPlaySrcs":{"1":"https://media.example.test/first.mp4","2":"https://media.example.test/second.mp4"}}</script>`
	if media := parseAIVideoURL(page, "https://huangguoai.com/video/fixture/ep-2/"); media != "https://media.example.test/second.mp4" {
		t.Fatalf("selected episode 2 returned the page default: %q", media)
	}
}

func TestHuangguoAIPlaybackRejectsWrongOrMissingEpisode(t *testing.T) {
	for _, data := range []string{
		`{"id":"fixture","ep":1,"videoSrc":"https://media.example.test/first.mp4","epPlaySrcs":{"1":"https://media.example.test/first.mp4"}}`,
		`{"id":"other","ep":2,"videoSrc":"https://media.example.test/other.mp4"}`,
		`{"id":"fixture","ep":2,"videoSrc":"https://media.example.test/second.mp4","epPlaySrcs":{"2":""}}`,
		`{"id":"fixture","videoSrc":"https://media.example.test/first.mp4","epPlaySrcs":{"1":"https://media.example.test/first.mp4"}}`,
		`{"id":"fixture","ep":`,
	} {
		page := `<script id="videoInitialData" type="application/json">` + data + `</script><div data-play-src="https://media.example.test/fallback.mp4"></div>`
		if media := parseAIVideoURL(page, "https://huangguoai.com/video/fixture/ep-2/"); media != "" {
			t.Errorf("unavailable selected episode fell back to another video: %q", media)
		}
	}
}

func TestHuangguoAIPlaybackAcceptsMatchingPageAndLegacyMarkup(t *testing.T) {
	for _, page := range []string{
		`<script id="videoInitialData">{"id":"fixture","ep":2,"videoSrc":"https://media.example.test/second.mp4"}</script>`,
		`<div data-play-src="https://media.example.test/second.mp4"></div>`,
	} {
		if media := parseAIVideoURL(page, "https://huangguoai.com/video/fixture/ep-2/"); media != "https://media.example.test/second.mp4" {
			t.Errorf("matching selected-episode media was lost: %q", media)
		}
	}
}

func TestHuangguoAIPlaybackValidatesRequestedDramaAndPage(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		path  string
		data  string
		valid bool
	}{
		{"old-recommendation", "/video/other/ep-2/", `{"id":"other","ep":2,"videoSrc":"https://media.example.test/other.mp4"}`, false},
		{"redirected-first-episode", "/video/fixture/", `{"id":"fixture","ep":1,"videoSrc":"https://media.example.test/first.mp4"}`, false},
		{"wrong-response-drama", "/video/fixture/ep-2/", `{"id":"other","ep":2,"videoSrc":"https://media.example.test/other.mp4"}`, false},
		{"missing-selected-episode", "/video/fixture/ep-2/", `{"id":"fixture","ep":1,"videoSrc":"https://media.example.test/first.mp4"}`, false},
		{"matching-selected-episode", "/video/fixture/ep-2/", `{"id":"fixture","ep":2,"videoSrc":"https://media.example.test/second.mp4"}`, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			requests := 0
			d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
				requests++
				if request.URL.Path != fixture.path {
					t.Fatalf("unexpected resource: %s", request.URL.Path)
				}
				return sourceFixtureResponse(request, 200, `<script id="videoInitialData">`+fixture.data+`</script>`), nil
			})
			media, err := d.resolveProviderMedia(context.Background(), Task{
				DramaID: providerDramaID(sourceHuangguoAI, "fixture"), Index: 2,
				Chapter: Chapter{Source: sourceHuangguoAI, PageURL: "https://huangguoai.com" + fixture.path, CurrentEpisode: rawEpisode(2)},
			})
			if fixture.valid {
				if err != nil || media.URL != "https://media.example.test/second.mp4" {
					t.Fatalf("matching playback failed: %+v, %v", media, err)
				}
			} else if err == nil {
				t.Fatalf("mismatched playback returned a video: %+v", media)
			}
			if requests != 1 {
				t.Fatalf("unexpected image/video or fallback requests: %d", requests)
			}
		})
	}
}
