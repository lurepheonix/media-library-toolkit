package shared

import "testing"

func TestSanitizeComponent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"AC/DC", "AC-DC"},
		{`a\b`, "a-b"},
		{"Who?", "Who"},
		{"Title: Subtitle", "Title Subtitle"},
		{`a<b>c"d|e*f?g:h`, "abcdefgh"},
		{"trailing dot.", "trailing dot"},
		{"  padded  ", "padded"},
		{"...Baby", "Baby"}, // leading dots are stripped (would be hidden files)
		{".hidden", "hidden"},
		{"???", ""},
		{"...", ""},
		{"a\x01b\x00c", "abc"},
		{"plain", "plain"},
	}
	for _, c := range cases {
		if got := SanitizeComponent(c.in); got != c.want {
			t.Errorf("SanitizeComponent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsMusicFile(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"01. Song.flac", true},
		{"song.MP3", true},
		{".DS_Store", false},
		{".LSOverride", false},
		{"._01. Song.flac", false}, // AppleDouble artifact, despite audio extension
		{"cover.jpg", false},
		{"song.txt", false},
	}
	for _, c := range cases {
		if got := IsMusicFile(c.in); got != c.want {
			t.Errorf("IsMusicFile(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
