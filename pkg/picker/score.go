// Package picker implements the interactive fuzzy selector.
//
// It is written from scratch rather than shelling out to fzf: requiring an
// external binary is exactly the friction that makes kubectx's picker
// unavailable on a fresh machine. Scoring, filtering, and the key state machine
// are pure functions so they can be tested without a terminal.
package picker

import (
	"sort"
	"strings"
	"unicode"
)

// Scoring weights, shaped after fzf's. gapStart + bonusConsecutive must exceed
// bonusBoundary, or "p-r-o" would tie or beat "prod" for the query "pro" by
// collecting a word-start bonus at every hyphen.
const (
	scoreMatch = 16
	// bonusFirstChar multiplies the boundary bonus at position 0, so a prefix
	// match outranks the same match in the middle of a longer name.
	bonusFirstChar   = 2
	bonusBoundary    = 8
	bonusCamel       = 7
	bonusConsecutive = 4
	gapStart         = 9
	gapExtension     = 1
	penaltyGapMax    = 25
)

// saturatedGap is the gap from which the penalty stops growing.
const saturatedGap = 1 + (penaltyGapMax-gapStart+gapExtension-1)/gapExtension

// boundaryChars separate words in the names this picker deals with: context
// names, namespaces, cluster ARNs.
const boundaryChars = "-_./: "

// Match is one item that survived filtering.
type Match struct {
	// Index is the position of the item in the original slice.
	Index int
	// Score ranks the match; higher is better.
	Score int
	// Positions lists the rune offsets in the target that the query matched,
	// for highlighting.
	Positions []int
}

// Score fuzzy-matches query against target, case-insensitively.
//
// It returns the score, the matched rune positions, and whether every query
// character was found in order. An empty query matches everything with score 0.
func Score(query, target string) (int, []int, bool) {
	q := []rune(strings.ToLower(query))
	t := []rune(target)
	lower := []rune(strings.ToLower(target))

	if len(q) == 0 {
		return 0, nil, true
	}
	if len(q) > len(t) {
		return 0, nil, false
	}

	// prev[j] is the score of the best alignment of the query up to the
	// previous character that ends with a match at target position j.
	// parent[i][j] records which position the previous query character matched,
	// so the highlight positions can be recovered by walking back.
	const noMatch = -1 << 30
	prev := make([]int, len(t))
	cur := make([]int, len(t))
	parent := make([][]int, len(q))
	for i := range parent {
		parent[i] = make([]int, len(t))
		for j := range parent[i] {
			parent[i][j] = -1
		}
	}

	for i := range q {
		// A nearer predecessor pays less gap penalty, so the top scorer is not
		// always best. Past saturatedGap the penalty is flat: keep a running best
		// there and compare only the nearer ones.
		farVal, farAt := noMatch, -1

		for j := range t {
			if k := j - saturatedGap - 1; i > 0 && k >= 0 && prev[k] > noMatch && (farAt < 0 || prev[k] >= farVal) {
				farVal, farAt = prev[k], k
			}

			cur[j] = noMatch
			if lower[j] != q[i] {
				continue
			}

			if i == 0 {
				// The first query character may match anywhere.
				cur[j] = scoreMatch + charBonus(t, j)
				continue
			}

			best, bestAt := noMatch, -1
			if farAt >= 0 {
				best, bestAt = farVal-penaltyGapMax, farAt
			}
			// Ties go to the rightmost candidate, the one with the smallest gap.
			for k := max(0, j-saturatedGap); k < j; k++ {
				if prev[k] == noMatch {
					continue
				}
				if s := prev[k] + transition(j-k-1); s >= best {
					best, bestAt = s, k
				}
			}
			if bestAt < 0 {
				continue // nowhere for the previous character to have matched
			}
			cur[j] = best + scoreMatch + charBonus(t, j)
			parent[i][j] = bestAt
		}
		prev, cur = cur, prev
	}

	// The best full match is the highest-scoring end position of the last
	// query character.
	best, bestAt := noMatch, -1
	for j := range t {
		if prev[j] > best {
			best, bestAt = prev[j], j
		}
	}
	if bestAt < 0 || best == noMatch {
		return 0, nil, false
	}

	positions := make([]int, len(q))
	at := bestAt
	for i := len(q) - 1; i >= 0; i-- {
		positions[i] = at
		at = parent[i][at]
	}
	return best, positions, true
}

// transition is what joining two matched characters gap runes apart earns.
func transition(gap int) int {
	if gap == 0 {
		return bonusConsecutive
	}
	return -min(gapStart+(gap-1)*gapExtension, penaltyGapMax)
}

// charBonus rewards matches that land at a word boundary or a camelCase hump,
// which is where a human aiming at a name would type.
func charBonus(target []rune, j int) int {
	if j == 0 {
		return bonusBoundary * bonusFirstChar
	}
	if strings.ContainsRune(boundaryChars, target[j-1]) {
		return bonusBoundary
	}
	if unicode.IsUpper(target[j]) && unicode.IsLower(target[j-1]) {
		return bonusCamel
	}
	return 0
}

// Filter scores every candidate against query and returns the matches, best
// first.
//
// Equal scores are broken by length — the shorter name contains less noise
// around the match — and then by input order. An empty query is not ranked at
// all, so the list stays exactly as it came in.
func Filter(query string, candidates []string) []Match {
	matches := make([]Match, 0, len(candidates))
	for i, candidate := range candidates {
		score, positions, ok := Score(query, candidate)
		if !ok {
			continue
		}
		matches = append(matches, Match{Index: i, Score: score, Positions: positions})
	}

	if query == "" {
		return matches
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return len(candidates[matches[i].Index]) < len(candidates[matches[j].Index])
	})
	return matches
}
