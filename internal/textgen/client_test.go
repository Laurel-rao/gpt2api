package textgen

import "testing"

func TestNormalizeModel(t *testing.T) {
	cases := map[string]string{
		"gpt-5.4":      "gpt-5.4",
		"gpt-5-4":      "gpt-5.4",
		"gpt-5-4-mini": "gpt-5.4-mini",
		"gpt-5.5":      "gpt-5.5",
		"gpt-5":        "gpt-5.4",
		"":             "gpt-5.4",
	}
	for in, want := range cases {
		if got := normalizeModel(in, defaultModel); got != want {
			t.Fatalf("normalizeModel(%q) = %q, want %q", in, got, want)
		}
	}
}
