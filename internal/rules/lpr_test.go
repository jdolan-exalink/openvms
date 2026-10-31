package rules_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/rules"
)

func TestMatchesPlates(t *testing.T) {
	wanted := map[string]struct{}{
		"AB123CD": {},
		"AA000BB": {},
	}

	tests := []struct {
		name   string
		plates []string
		wanted map[string]struct{}
		match  bool
	}{
		{"exact match", []string{"AB123CD"}, wanted, true},
		{"lowercase match", []string{"ab123cd"}, wanted, true},
		{"trimmed match", []string{"  AB123CD  "}, wanted, true},
		{"second plate matches", []string{"ZZ999ZZ", "AA000BB"}, wanted, true},
		{"no match", []string{"XX111YY"}, wanted, false},
		{"empty plates", []string{}, wanted, false},
		{"empty wanted set", []string{"AB123CD"}, map[string]struct{}{}, false},
		{"nil wanted set", []string{"AB123CD"}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rules.MatchesPlates(tt.plates, tt.wanted)
			if got != tt.match {
				t.Errorf("MatchesPlates(%v, %v) = %v, want %v", tt.plates, tt.wanted, got, tt.match)
			}
		})
	}
}

func TestHasLprConditions(t *testing.T) {
	c1 := rules.Conditions{}
	if c1.HasLprConditions() {
		t.Errorf("expected HasLprConditions() = false for empty conditions")
	}

	c2 := rules.Conditions{LprWatchlistIDs: []uuid.UUID{uuid.New()}}
	if !c2.HasLprConditions() {
		t.Errorf("expected HasLprConditions() = true when LprWatchlistIDs is present")
	}
}
