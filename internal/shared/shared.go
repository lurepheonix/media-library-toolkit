// Package shared holds helpers common to all meltk subcommands.
package shared

import (
	"path/filepath"
	"strconv"
	"strings"
)

// SupportedExtensions is the set of audio files meltk operates on.
var SupportedExtensions = map[string]bool{
	".mp3":  true,
	".flac": true,
	".aac":  true,
	".m4a":  true,
	".opus": true,
	".ogg":  true,
	".wav":  true,
}

// IsAudio reports whether path has a supported audio extension.
func IsAudio(path string) bool {
	return SupportedExtensions[strings.ToLower(filepath.Ext(path))]
}

// macOS artifact files that must never be treated as music, even when
// their names carry an audio extension (e.g. "._01. Song.flac").
func IsArtifact(base string) bool {
	base = filepath.Base(base)
	return base == ".DS_Store" || base == ".LSOverride" || strings.HasPrefix(base, "._")
}

// IsMusicFile reports whether path is a real audio file to process:
// supported extension and not a macOS artifact file.
func IsMusicFile(path string) bool {
	return !IsArtifact(path) && IsAudio(path)
}

// FileSkipReason explains why a scanned file is skipped.
func FileSkipReason(path string) string {
	if IsArtifact(path) {
		return "macOS artifact file"
	}
	return "unsupported file type"
}

// SanitizeComponent removes characters illegal in path components so results
// stay portable (Android SD cards, MTP, Windows/FAT/exFAT).
// Tag-derived "/" and "\" become "-" (never "_", to avoid confusion with
// the restore placeholder mechanism); the rest of the portable-illegal set
// (< > : " | ? *) and control characters are stripped entirely.
// Surrounding spaces and leading/trailing dots are trimmed (leading dots
// would create hidden files on Android/Linux/macOS).
func SanitizeComponent(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 {
			return -1
		}
		switch r {
		case '<', '>', ':', '"', '|', '?', '*':
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".")
	return strings.TrimSpace(s)
}

// FirstTag returns the first non-blank value for any of keys.
func FirstTag(tags map[string][]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := tags[k]; ok {
			for _, s := range v {
				if strings.TrimSpace(s) != "" {
					return s
				}
			}
		}
	}
	return ""
}

// ParseYear extracts the first YYYY (1000-2999) from a tag value, so
// "2009", "2009-05-01" and "2021 Remaster" all yield a year.
func ParseYear(s string) string {
	for i := 0; i+4 <= len(s); i++ {
		if !IsDigit(s[i]) || !IsDigit(s[i+1]) || !IsDigit(s[i+2]) || !IsDigit(s[i+3]) {
			continue
		}
		if i > 0 && IsDigit(s[i-1]) {
			continue
		}
		if i+4 < len(s) && IsDigit(s[i+4]) {
			continue
		}
		y := s[i : i+4]
		if y >= "1000" && y <= "2999" {
			return y
		}
	}
	return ""
}

// ParseTrackNumber extracts a track number from values like "3", "03",
// "3/12" or " 7 ". Only the leading digit run is used; the result must be
// in 1..999.
func ParseTrackNumber(s string) (int, bool) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && IsDigit(s[i]) {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil || n < 1 || n > 999 {
		return 0, false
	}
	return n, true
}

func IsDigit(c byte) bool { return c >= '0' && c <= '9' }

// MostCommon returns the most frequent value; ties go to first-seen order.
func MostCommon(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	counts := map[string]int{}
	best, bestN := "", 0
	for _, v := range vals {
		counts[v]++
		if counts[v] > bestN {
			best, bestN = v, counts[v]
		}
	}
	return best
}

func DistinctCount(vals []string) int {
	seen := map[string]bool{}
	for _, v := range vals {
		seen[v] = true
	}
	return len(seen)
}

func PathDepth(p string) int {
	return strings.Count(p, string(filepath.Separator))
}
