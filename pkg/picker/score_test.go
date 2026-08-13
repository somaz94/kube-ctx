package picker

import (
	"reflect"
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
	candidates := []string{"dev", "prod", "staging"}

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

func BenchmarkScore(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Score("pek", "arn:aws:eks:ap-northeast-2:123456789012:cluster/prod-eks")
	}
}
