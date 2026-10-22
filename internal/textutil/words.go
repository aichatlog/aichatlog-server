// Package textutil counts and truncates "words" in mixed CJK / Latin text.
//
// strings.Fields treats a whole Chinese paragraph as one word, so word-based
// thresholds and truncation were badly off for Chinese conversations. Here each
// CJK character counts as one word and every other run of non-space characters
// counts as one word.
package textutil

import (
	"unicode"
	"unicode/utf8"
)

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// CountWords returns the number of words in s.
func CountWords(s string) int {
	n, inWord := 0, false
	for _, r := range s {
		switch {
		case isCJK(r):
			n++
			inWord = false
		case unicode.IsSpace(r) || unicode.IsPunct(r) && r > utf8.RuneSelf:
			inWord = false
		default:
			if !inWord {
				n++
				inWord = true
			}
		}
	}
	return n
}

// TruncateWords returns the longest prefix of s with at most max words and
// whether anything was cut off.
func TruncateWords(s string, max int) (string, bool) {
	n, inWord := 0, false
	for i, r := range s {
		start := false
		switch {
		case isCJK(r):
			start, inWord = true, false
		case unicode.IsSpace(r) || unicode.IsPunct(r) && r > utf8.RuneSelf:
			inWord = false
		default:
			if !inWord {
				start, inWord = true, true
			}
		}
		if start {
			if n == max {
				return s[:i], true
			}
			n++
		}
	}
	return s, false
}
