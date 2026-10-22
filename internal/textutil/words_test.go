package textutil

import "testing"

func TestCountWords(t *testing.T) {
	cases := map[string]int{
		"":                          0,
		"hello world":               2,
		"在鲲鹏服务器上部署":                 9,
		"部署 Immich v3.3.3 到 HuaWei": 6,
		"中文，标点。不算":                  6,
	}
	for in, want := range cases {
		if got := CountWords(in); got != want {
			t.Errorf("CountWords(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTruncateWords(t *testing.T) {
	got, cut := TruncateWords("部署 Immich 到服务器", 3)
	if got != "部署 Immich " || !cut {
		t.Errorf("got %q cut=%v", got, cut)
	}
	if got, cut := TruncateWords("abc def", 5); got != "abc def" || cut {
		t.Errorf("got %q cut=%v", got, cut)
	}
}
