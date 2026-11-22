package core

import (
	"errors"
	"net/http"
	"testing"
)

// 站点 2026-11 起列表改为 /api/videos?cate_id=N，data 里是 items 与 pagination；
// 旧的 /api/videos/category/{slug} 一律回「分类不存在」。
func TestHuangguoAICatalogUsesCateIDEndpoint(t *testing.T) {
	var sawPath, sawCategory string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		sawPath, sawCategory = request.URL.Path, request.URL.Query().Get("cate_id")
		if request.URL.Path != "/api/videos" {
			return nil, errors.New("catalog must use the cate_id endpoint")
		}
		if request.URL.Query().Get("page") != "1" || request.URL.Query().Get("sort") != "hot" {
			return nil, errors.New("catalog must carry sort and page")
		}
		return sourceFixtureResponse(request, 200,
			`{"data":{"items":[{"id":12,"title":"合成短剧","cover":"/upload_01/synthetic.jpg","episode_count":21,"score":9.6,"tags":["都市"]}],"pagination":{"page":1,"pages":3,"size":24,"total":48}},"status":1}`), nil
	})
	items, more, err := d.fetchHuangguoAICatalogPage(t.Context(), 1, "24")
	if err != nil {
		t.Fatalf("catalog failed: %v", err)
	}
	if sawPath != "/api/videos" || sawCategory != "24" {
		t.Fatalf("unexpected catalog request path=%q cate_id=%q", sawPath, sawCategory)
	}
	if len(items) != 1 {
		t.Fatalf("catalog parsed %d items", len(items))
	}
	item := items[0]
	if item.SourceID != "12" || item.Title != "合成短剧" || item.TotalEpisode != "21" || item.CategoryName != "AI 短剧" {
		t.Fatalf("catalog card fields were lost: %+v", item)
	}
	if item.Cover == "" {
		t.Fatalf("catalog cover was dropped: %+v", item)
	}
	if !more {
		t.Fatal("catalog reported no more pages for a 48-item category")
	}
}

func TestHuangguoAICategoriesFollowSiteIDs(t *testing.T) {
	if len(nativeAICategories) != 2 {
		t.Fatalf("unexpected category table: %+v", nativeAICategories)
	}
	for _, entry := range nativeAICategories {
		if entry.ID != "24" && entry.ID != "25" {
			t.Fatalf("retired category slug is still offered: %+v", entry)
		}
		if !validNativeCategory(sourceHuangguoAI, entry.ID) {
			t.Fatalf("offered category is rejected by its own validator: %+v", entry)
		}
	}
	for _, retired := range []string{"ai-duanju", "ai-manju", "ai-huanlian", "ai-mogai"} {
		if validNativeCategory(sourceHuangguoAI, retired) {
			t.Fatalf("retired category slug is still accepted: %s", retired)
		}
	}
}
