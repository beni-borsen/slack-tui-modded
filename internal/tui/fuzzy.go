package tui

import (
	"slices"
	"strings"
	"unicode"
)

// fuzzyRank returns the indexes of candidates that contain every rune of
// query in order (case-insensitive), best match first. Ties keep the
// candidates' original order.
func fuzzyRank(query string, candidates []string) []int {
	q := []rune(strings.ToLower(query))
	type hit struct{ idx, score int }
	var hits []hit
	for i, c := range candidates {
		if s, ok := fuzzyScore(q, []rune(c)); ok {
			hits = append(hits, hit{i, s})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

// fuzzyScore matches q greedily against s. Matches at the start of a word
// and runs of consecutive matches score higher; skipped runes cost a little.
func fuzzyScore(q, s []rune) (int, bool) {
	if len(q) == 0 {
		return 0, true
	}
	score, qi, prev := 0, 0, -2
	for si, r := range s {
		if qi == len(q) {
			break
		}
		if unicode.ToLower(r) != q[qi] {
			continue
		}
		switch {
		case si == 0:
			score += 12
		case isBoundary(s[si-1], r):
			score += 8
		}
		if si == prev+1 {
			score += 6
		}
		if prev >= 0 {
			score -= min(si-prev-1, 5) // gap penalty
		}
		score += 2
		prev = si
		qi++
	}
	if qi < len(q) {
		return 0, false
	}
	return score - len(s)/8, true // slight preference for shorter names
}

func isBoundary(before, r rune) bool {
	if !unicode.IsLetter(before) && !unicode.IsDigit(before) {
		return true
	}
	return unicode.IsLower(before) && unicode.IsUpper(r)
}
