package core

import (
	"reflect"
	"testing"
)

func TestOrderProviderCandidatesPrefersRememberedHost(t *testing.T) {
	candidates := []string{
		"https://huangguoai.com/a",
		"https://ttvoij.ediayikma.cc/a",
		"https://thu.agdkczeyx.cc/a",
		"https://pku.ediayikma.cc/a",
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
				"https://thu.agdkczeyx.cc/a",
				"https://huangguoai.com/a",
				"https://ttvoij.ediayikma.cc/a",
				"https://pku.ediayikma.cc/a",
			},
		},
		{
			name:      "already first stays unchanged",
			preferred: "https://huangguoai.com",
			source:    sourceHuangguoAI,
			want:      candidates,
		},
		{
			name:      "unknown remembered host leaves order",
			preferred: "https://not-a-candidate.example",
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
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			hosts := map[string]string{}
			if testCase.preferred != "" {
				hosts[sourceHuangguoAI] = testCase.preferred
			}
			d := &Downloader{providerHosts: hosts}
			got := d.orderProviderCandidates(candidates, testCase.source)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("got %v, want %v", got, testCase.want)
			}
			if len(candidates) != 4 || candidates[0] != "https://huangguoai.com/a" {
				t.Fatalf("input slice was mutated: %v", candidates)
			}
		})
	}
}

func TestOrderProviderCandidatesLeavesShortListsAlone(t *testing.T) {
	d := &Downloader{providerHosts: map[string]string{sourceHuangguoAI: "https://thu.agdkczeyx.cc"}}
	single := []string{"https://huangguoai.com/a"}
	got := d.orderProviderCandidates(single, sourceHuangguoAI)
	if !reflect.DeepEqual(got, single) {
		t.Fatalf("got %v, want %v", got, single)
	}
}
