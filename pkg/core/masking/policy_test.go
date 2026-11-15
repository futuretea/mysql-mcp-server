package masking

import (
	"strings"
	"testing"
)

func TestParseJSONAbsentAndEmpty(t *testing.T) {
	if _, err := ParseJSON(""); err == nil || err.Error() != InvalidConfigMessage {
		t.Fatalf("empty JSON error = %v, want %s", err, InvalidConfigMessage)
	}
	if _, err := ParseJSON("   "); err == nil || err.Error() != InvalidConfigMessage {
		t.Fatalf("blank JSON error = %v, want %s", err, InvalidConfigMessage)
	}
	policy := Disabled()
	if policy.Enabled() {
		t.Fatal("disabled policy is active")
	}
}

func TestParseJSONRejectsUnknownFieldAndEmptyEnabledPolicy(t *testing.T) {
	secret := "synthetic-token-value"
	for _, raw := range []string{
		`{"enabled":true,"names":[],"extra":"` + secret + `"}`,
		`{"enabled":true}`,
		`{"enabled":true,"names":[""]}`,
		`{"enabled":true,"value_patterns":["a*"]}`,
		`{"enabled":true,"value_patterns":[""]}`,
		`{"enabled":true,"placeholder":""}`,
		`{"enabled":true} extra`,
	} {
		_, err := ParseJSON(raw)
		if err == nil || err.Error() != InvalidConfigMessage || strings.Contains(err.Error(), secret) {
			t.Fatalf("ParseJSON(%s) error = %v", raw, err)
		}
	}
}

func TestParseJSONEnabledRequiresExplicitTrue(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":false,"values":["synthetic-token-value"]}`)
	if err != nil || policy.Enabled() {
		t.Fatalf("enabled false policy = %+v, %v", policy, err)
	}
	policy, err = ParseJSON(`{"names":["synthetic_field"]}`)
	if err != nil || policy.Enabled() {
		t.Fatalf("omitted enabled policy = %+v, %v", policy, err)
	}
	policy, err = ParseJSON(`{"enabled":true,"names":["synthetic_field"],"placeholder":"[masked]"}`)
	if err != nil || !policy.Enabled() || !policy.MatchName("synthetic_field") || policy.MatchName("other") {
		t.Fatalf("enabled policy = enabled %t match %t, err %v", policy.Enabled(), policy.MatchName("synthetic_field"), err)
	}
}

func TestNamePatternIsAnchoredAndShortLiteralIsKept(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"name_patterns":["id"],"values":["x"],"value_patterns":["z+"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.MatchName("id") || policy.MatchName("identity") {
		t.Fatal("name pattern matched a longer name or missed the whole name")
	}
	got, err := policy.MaskText("axb zzz")
	if err != nil || got != "a[masked]b [masked]" {
		t.Fatalf("MaskText = %q, %v", got, err)
	}
}

func TestSpanReplacementDoesNotCascade(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"values":["ab","bc"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := policy.MaskText("abcd")
	if err != nil || got != "[masked]d" {
		t.Fatalf("MaskText = %q, %v", got, err)
	}
	if strings.Contains(got, "ab") || strings.Contains(got, "bc") {
		t.Fatalf("original spans remain in %q", got)
	}
}
