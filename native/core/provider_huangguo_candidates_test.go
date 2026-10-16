package core

import (
	"reflect"
	"testing"
)

func TestOrderProviderCandidatesPrefersRememberedHost(t *testing.T) {
	rawURL := "https://huangguoai.com/api/videos/category/ai-manju?sort=hot&page=1&size=24"
	candidates := []string{
		"https://huangguoai.com/api/videos/category/ai-manju?sort=hot&page=1&size=24",
		"https://ttvoij.ediayikma.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
		"https://thu.agdkczeyx.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
		"https://pku.ediayikma.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
	}
	cases := []struct {
		name      string
		preferred string
		source    string
		want      []string
	}{
		{
			name:      "remembered host moves to front",
			preferred: "https://thu.agdkczeyx.cc",
			source:    sourceHuangguoAI,
			want: []string{
				"https://thu.agdkczeyx.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
				"https://huangguoai.com/api/videos/category/ai-manju?sort=hot&page=1&size=24",
				"https://ttvoij.ediayikma.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
				"https://pku.ediayikma.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
			},
		},
		{
			name:      "remembered host absent from the list is prepended",
			preferred: "https://86ed8d.ediayikma.cc",
			source:    sourceHuangguoAI,
			want: []string{
				"https://86ed8d.ediayikma.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
				"https://huangguoai.com/api/videos/category/ai-manju?sort=hot&page=1&size=24",
				"https://ttvoij.ediayikma.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
				"https://thu.agdkczeyx.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
				"https://pku.ediayikma.cc/api/videos/category/ai-manju?sort=hot&page=1&size=24",
			},
		},
		{
			name:      "already first stays unchanged",
			preferred: "https://huangguoai.com",
			source:    sourceHuangguoAI,
			want:      candidates,
		},
		{
			name:      "empty preferred leaves order",
			preferred: "",
			source:    sourceHuangguoAI,
			want:      candidates,
		},
		{
			name:      "empty source leaves order",
			preferred: "https://thu.agdkczeyx.cc",
			source:    "",
			want:      candidates,
		},
		{
			name:      "insecure remembered origin is ignored",
			preferred: "http://thu.agdkczeyx.cc",
			source:    sourceHuangguoAI,
			want:      candidates,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			hosts := map[string]string{}
			if testCase.preferred != "" {
				hosts[sourceHuangguoAI] = testCase.preferred
			}
			d := &Downloader{providerHosts: hosts}
			got := d.orderProviderCandidates(rawURL, candidates, testCase.source)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("got %v, want %v", got, testCase.want)
			}
			if len(candidates) != 4 || candidates[0] != rawURL {
				t.Fatalf("input slice was mutated: %v", candidates)
			}
		})
	}
}

func TestOrderProviderCandidatesLeavesShortListsAlone(t *testing.T) {
	rawURL := "https://huangguoai.com/a"
	d := &Downloader{providerHosts: map[string]string{sourceHuangguoAI: "https://thu.agdkczeyx.cc"}}
	single := []string{rawURL}
	got := d.orderProviderCandidates(rawURL, single, sourceHuangguoAI)
	if !reflect.DeepEqual(got, single) {
		t.Fatalf("got %v, want %v", got, single)
	}
}
