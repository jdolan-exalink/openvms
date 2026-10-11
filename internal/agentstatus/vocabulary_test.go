package agentstatus_test

import (
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agentstatus"
)

func sorted(in []string) []string {
	out := slices.Clone(in)
	sort.Strings(out)
	return out
}

func quotedList(t *testing.T, list string) []string {
	t.Helper()
	var out []string
	for _, part := range strings.Split(list, ",") {
		part = strings.Trim(strings.TrimSpace(part), `'"`)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no values in %q", list)
	}
	return out
}

// The status vocabulary lives in agentstatus.Statuses. The database CHECK and the OpenAPI enum
// repeat it because they have to; this test fails when either drifts.
func TestVocabularyMatchesTheMigrationAndTheContract(t *testing.T) {
	want := sorted(agentstatus.Statuses)

	migration, err := os.ReadFile("../../migrations/00044_agent_heartbeats.sql")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`status\s+text[^;]*?CHECK\s*\(\s*status\s+IN\s*\(([^)]*)\)`).FindSubmatch(migration)
	if m == nil {
		t.Fatal("the agent_heartbeats status CHECK was not found in migration 00044")
	}
	if got := sorted(quotedList(t, string(m[1]))); !slices.Equal(got, want) {
		t.Errorf("migration CHECK = %v, agentstatus.Statuses = %v", got, want)
	}

	contract, err := os.ReadFile("../../packages/api-contract/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	o := regexp.MustCompile(`(?m)^    AgentStatus:\n(?:      .*\n)*?      enum: \[([^\]]*)\]`).FindSubmatch(contract)
	if o == nil {
		t.Fatal("the AgentStatus enum was not found in the OpenAPI contract")
	}
	if got := sorted(quotedList(t, string(o[1]))); !slices.Equal(got, want) {
		t.Errorf("OpenAPI AgentStatus = %v, agentstatus.Statuses = %v", got, want)
	}
}

func TestValidAcceptsExactlyTheVocabulary(t *testing.T) {
	for _, s := range agentstatus.Statuses {
		if !agentstatus.Valid(s) {
			t.Errorf("Valid(%q) = false", s)
		}
	}
	for _, s := range []string{"", "ONLINE", "on fire", "online "} {
		if agentstatus.Valid(s) {
			t.Errorf("Valid(%q) = true", s)
		}
	}
}

func TestValidateChecksStatusAndVersionOnce(t *testing.T) {
	long := strings.Repeat("v", agentstatus.MaxVersionLen)
	if err := (agentstatus.Heartbeat{Version: long, Status: "online"}).Validate(); err != nil {
		t.Fatalf("a version of exactly MaxVersionLen bytes: %v", err)
	}
	for name, hb := range map[string]agentstatus.Heartbeat{
		"version too long": {Version: long + "v", Status: "online"},
		"unknown status":   {Status: "on fire"},
		"empty status":     {},
	} {
		if err := hb.Validate(); err == nil || !strings.Contains(err.Error(), "invalid heartbeat") {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}
