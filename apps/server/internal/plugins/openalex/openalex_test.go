package openalex

import "testing"

func TestWorkKey(t *testing.T) {
	for raw, want := range map[string]string{
		"W2626778328":                          "W2626778328",
		"https://openalex.org/W2626778328":     "W2626778328",
		"10.1038/nature14539":                  "doi:10.1038/nature14539",
		"https://doi.org/10.1038/nature14539.": "doi:10.1038/nature14539",
		"arXiv:1706.03762v7":                   "doi:10.48550/arXiv.1706.03762",
		"https://arxiv.org/abs/1706.03762":     "doi:10.48550/arXiv.1706.03762",
	} {
		if got, err := workKey(raw); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", raw, got, err, want)
		}
	}
	if _, err := workKey("a paper"); err == nil {
		t.Error("nonsense taken as an id")
	}
}

func TestAbstractText(t *testing.T) {
	got := abstractText(map[string][]int{"is": {1}, "all": {2}, "Attention": {0}, "need": {5}, "you": {4}, "All": {3}})
	if got != "Attention is all All you need" {
		t.Fatalf("%q", got)
	}
	if abstractText(nil) != "" {
		t.Fatal("no index made words")
	}
}
