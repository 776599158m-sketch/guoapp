package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func huangguoRankingFixture(board rankingBoard, addresses ...string) string {
	items := make([]map[string]any, 0, len(addresses))
	for index, address := range addresses {
		items = append(items, map[string]any{
			"position": index*2 + 1, "name": fmt.Sprintf("合成剧集%d", index+1), "url": address,
		})
	}
	data, _ := json.Marshal(map[string]any{"@graph": []any{map[string]any{
		"@type": "ItemList", "@id": "https://huangguoai.com/ranks/" + board.path + "/#itemlist",
		"itemListElement": items,
	}}})
	return `<script type="application/ld+json">` + string(data) + `</script>`
}

func TestHuangguoAIRankingVideoLinksLoadAndOpenSelectedDrama(t *testing.T) {
	for _, boardID := range []string{"huangguo-hot", "huangguo-recommend", "huangguo-potential"} {
		t.Run(boardID, func(t *testing.T) {
			board, _ := findRankingBoard(boardID)
			body := huangguoRankingFixture(board, "https://huangguoai.com/video/12/", "/video/34/")
			var paths []string
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				paths = append(paths, request.URL.Path)
				switch request.URL.Path {
				case "/ranks/" + board.path + "/":
					return rankingHTTPResponse(request, 200, body), nil
				case "/detail/12/":
					return rankingHTTPResponse(request, 200, `<h1>合成剧集1</h1>
						<a class="hg-web-play__ep" href="/video/12/">1</a>
						<a class="hg-web-play__ep" href="/video/12/ep-2/">2</a>
						<a href="/video/other/ep-3/">其他剧集</a>`), nil
				default:
					t.Fatalf("unexpected request outside ranking and selected detail: %s", request.URL.Path)
					return nil, nil
				}
			})
			page, err := d.loadRankingPage(context.Background(), board, 1, false)
			if err != nil || len(page.Items) != 2 {
				t.Fatalf("ranking did not load: items=%d error=%v", len(page.Items), err)
			}
			if page.BoardID != boardID || page.Items[0].Rank != 1 || page.Items[1].Rank != 3 ||
				page.Items[0].Drama.ID != "huangguoai:12" || page.Items[1].Drama.ID != "huangguoai:34" ||
				page.HasMore || page.TotalPages != 1 {
				t.Fatalf("ranking identity, order, or pagination changed: %+v", page)
			}
			_, chapters, err := d.fetchHuangguoAIDetail(context.Background(), page.Items[0].Drama.SourceID)
			if err != nil || len(chapters) != 2 {
				t.Fatalf("selected ranking drama did not open: chapters=%d error=%v", len(chapters), err)
			}
			for _, chapter := range chapters {
				if !strings.HasPrefix(chapter.PageURL, "https://huangguoai.com/video/12/") {
					t.Fatalf("ranking opened another drama: %s", chapter.PageURL)
				}
			}
			if len(paths) != 2 {
				t.Fatalf("unexpected request count: %v", paths)
			}
		})
	}
}

func TestHuangguoAIRankingAcceptsLegacyAndCurrentDramaRoutes(t *testing.T) {
	board, _ := findRankingBoard("huangguo-hot")
	for _, address := range []string{
		"https://huangguoai.com/detail/12/", "https://huangguoai.com/video/12/",
		"/detail/12/", "/video/12/", "/video/12", "/video/12/?tracking=fixture",
	} {
		t.Run(address, func(t *testing.T) {
			page, err := parseHuangguoRanking(huangguoRankingFixture(board, address), board)
			if err != nil || len(page.Items) != 1 || page.Items[0].Drama.ID != "huangguoai:12" {
				t.Fatalf("valid drama route did not preserve identity: %+v %v", page, err)
			}
		})
	}
}

func TestHuangguoAIRankingRejectsEpisodesAndUnrelatedPages(t *testing.T) {
	board, _ := findRankingBoard("huangguo-hot")
	for _, address := range []string{
		"/video/12/ep-2/", "/video/12%2Fep-2/", "/detail/12/extra/",
		"/videos/12/", "/search/video/12/", "/ranks/hot/", "/video/",
		"javascript:/video/12/", "/video/12%0A/",
	} {
		t.Run(address, func(t *testing.T) {
			if _, err := parseHuangguoRanking(huangguoRankingFixture(board, address), board); err == nil {
				t.Fatal("non-drama URL became a ranking item")
			}
		})
	}
	if _, err := parseHuangguoRanking(huangguoRankingFixture(board, "/video/12/", "/detail/12/"), board); err == nil {
		t.Fatal("duplicate drama with different route prefixes was accepted")
	}
	other, _ := findRankingBoard("huangguo-recommend")
	if _, err := parseHuangguoRanking(huangguoRankingFixture(other, "/video/12/"), board); err == nil {
		t.Fatal("another board was accepted")
	}
}
