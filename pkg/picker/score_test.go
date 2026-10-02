package picker

import (
	"reflect"
	"strings"
	"testing"
)

func TestScoreMatching(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		target string
		want   bool
	}{
		{"empty query matches", "", "prod", true},
		{"exact", "prod", "prod", true},
		{"prefix", "pro", "prod-eks", true},
		{"subsequence", "pks", "prod-eks", true},
		{"case insensitive", "PROD", "prod-eks", true},
		{"query longer than target", "production", "prod", false},
		{"missing character", "prodx", "prod-eks", false},
		{"out of order", "dorp", "prod", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := Score(tt.query, tt.target)
			if ok != tt.want {
				t.Errorf("Score(%q, %q) ok = %v, want %v", tt.query, tt.target, ok, tt.want)
			}
		})
	}
}

func TestScorePositions(t *testing.T) {
	_, positions, ok := Score("pe", "prod-eks")
	if !ok {
		t.Fatal("expected a match")
	}
	if want := []int{0, 5}; !reflect.DeepEqual(positions, want) {
		t.Errorf("positions = %v, want %v", positions, want)
	}

	if _, positions, _ := Score("", "prod"); positions != nil {
		t.Errorf("empty query positions = %v, want nil", positions)
	}
}

func TestScoreRanking(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		better, worse string
	}{
		{"prefix beats mid-word", "eks", "eks-prod", "my-eks-prod"},
		{"word start beats mid-word", "p", "dev-prod", "development"},
		{"consecutive beats separator-hopping", "pro", "prod", "p-r-o"},
		{"tight beats spread", "abc", "abc-x", "a-b-c-x"},
		{"shorter gap beats longer gap", "ab", "axb", "axxxxxxb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			betterScore, _, ok := Score(tt.query, tt.better)
			if !ok {
				t.Fatalf("%q should match %q", tt.query, tt.better)
			}
			worseScore, _, ok := Score(tt.query, tt.worse)
			if !ok {
				t.Fatalf("%q should match %q", tt.query, tt.worse)
			}
			if betterScore <= worseScore {
				t.Errorf("%q scored %d, not better than %q at %d",
					tt.better, betterScore, tt.worse, worseScore)
			}
		})
	}
}

func TestScoreCamelCaseBonus(t *testing.T) {
	withHump, _, _ := Score("c", "devCluster")
	withoutHump, _, _ := Score("c", "devxcluster")
	if withHump <= withoutHump {
		t.Errorf("camelCase hump scored %d, not better than %d", withHump, withoutHump)
	}
}

func TestFilterRanksAndDropsNonMatches(t *testing.T) {
	candidates := []string{"dev", "prod-eks", "staging", "prod-gke"}

	got := Filter("prod", candidates)
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2", len(got))
	}
	for _, m := range got {
		if candidates[m.Index] != "prod-eks" && candidates[m.Index] != "prod-gke" {
			t.Errorf("unexpected match %q", candidates[m.Index])
		}
	}
}

func TestFilterEmptyQueryKeepsOrder(t *testing.T) {
	// Not in length order, so the length tie-break would reorder them if the
	// empty query were ranked at all.
	candidates := []string{"staging", "dev", "prod"}

	got := Filter("", candidates)
	if len(got) != len(candidates) {
		t.Fatalf("got %d matches, want %d", len(got), len(candidates))
	}
	for i, m := range got {
		if m.Index != i {
			t.Errorf("match %d has index %d; the input order should be preserved", i, m.Index)
		}
	}
}

func TestFilterNoMatches(t *testing.T) {
	if got := Filter("zzz", []string{"dev", "prod"}); len(got) != 0 {
		t.Errorf("got %v, want no matches", got)
	}
}

// The best predecessor is not simply the highest scorer: the gap penalty
// depends on distance, so a nearer, lower-scoring match can win. Taking the
// highest scorer first highlighted the "e" of "eu" for the query "eks".
func TestScoreFindsTheBestAlignmentNotTheGreedyOne(t *testing.T) {
	score, positions, ok := Score("eks", "eu-prod-eks")
	if !ok {
		t.Fatal("expected a match")
	}
	if want := []int{8, 9, 10}; !reflect.DeepEqual(positions, want) {
		t.Errorf("positions = %v, want %v", positions, want)
	}
	// The same word-start run must score the same wherever the word sits.
	if other, _, _ := Score("eks", "my-eks-prod"); score != other {
		t.Errorf("eu-prod-eks scored %d, my-eks-prod %d; want equal", score, other)
	}
}

// Score has to agree with an exhaustive search over every alignment, and its
// positions have to be an alignment that earns the score it reports.
func TestScoreMatchesExhaustiveSearch(t *testing.T) {
	targets := []string{
		"eu-prod-eks", "my-eks-prod", "a----xab", "prod", "p-r-o", "prod-eks",
		"kubernetes-admin@kubernetes", "cluster/prod-eks-apne2", "devCluster",
		"aXbXcXabc", "aaa" + strings.Repeat("-", 25) + "ab", "abababababababab",
		// One gap either side of where the penalty caps.
		"a" + strings.Repeat("x", 16) + "b", "a" + strings.Repeat("x", 17) + "b",
	}
	queries := []string{"e", "ab", "eks", "pro", "pek", "kad", "abc", "aab", "ka", "prod"}

	for _, target := range targets {
		for _, query := range queries {
			want, wantOK := bruteScore(query, target)
			got, positions, ok := Score(query, target)
			if ok != wantOK || got != want {
				t.Errorf("Score(%q, %q) = %d, %v; exhaustive search says %d, %v",
					query, target, got, ok, want, wantOK)
				continue
			}
			if ok && alignmentScore(query, target, positions) != got {
				t.Errorf("Score(%q, %q) positions %v do not earn %d", query, target, positions, got)
			}
		}
	}
}

// The DP compares predecessors within saturatedGap one by one and keeps a flat
// running best beyond it, which is only right if the cap starts exactly there.
func TestSaturatedGapIsWhereThePenaltyCaps(t *testing.T) {
	if transition(saturatedGap) != -penaltyGapMax || transition(saturatedGap-1) == -penaltyGapMax {
		t.Errorf("saturatedGap = %d is not the first gap paying penaltyGapMax", saturatedGap)
	}
}

// bruteScore tries every alignment of query in target with Score's weights.
func bruteScore(query, target string) (int, bool) {
	q := []rune(strings.ToLower(query))
	lower := []rune(strings.ToLower(target))
	best, found := 0, false
	var walk func(i, from int, positions []int)
	walk = func(i, from int, positions []int) {
		if i == len(q) {
			if s := alignmentScore(query, target, positions); !found || s > best {
				best, found = s, true
			}
			return
		}
		for j := from; j < len(lower); j++ {
			if lower[j] == q[i] {
				walk(i+1, j+1, append(positions, j))
			}
		}
	}
	walk(0, 0, nil)
	return best, found
}

// alignmentScore is what Score's weights award one particular alignment.
func alignmentScore(query, target string, positions []int) int {
	t := []rune(target)
	total := 0
	for i, j := range positions {
		total += scoreMatch + charBonus(t, j)
		if i == 0 {
			continue
		}
		if gap := j - positions[i-1] - 1; gap == 0 {
			total += bonusConsecutive
		} else {
			total -= min(gapStart+(gap-1)*gapExtension, penaltyGapMax)
		}
	}
	return total
}

func BenchmarkScore(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Score("pek", "arn:aws:eks:ap-northeast-2:123456789012:cluster/prod-eks")
	}
}
