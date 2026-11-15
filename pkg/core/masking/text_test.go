package masking

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNestedJSONMasksDecodedKeysAndPreservesUnmatchedDocument(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"names":["synthetic_field"]}`)
	if err != nil {
		t.Fatal(err)
	}
	original := `{"n":1,"title":"ordinary-visible"}`
	got, err := policy.MaskText(original, nil)
	if err != nil || got != original {
		t.Fatalf("unmatched JSON = %q, %v", got, err)
	}

	got, err = policy.MaskText("{\"syn\\u0074hetic_field\":\"ordinary-visible\",\"keep\\u0022x\":1}", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "synthetic_field") || strings.Contains(got, "ordinary-visible") {
		t.Fatalf("decoded field leaked: %s", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["[masked]"] != "[masked]" {
		t.Fatalf("matched string = %#v", decoded["[masked]"])
	}
	if decoded["keep\"x"] != json.Number("1") && decoded["keep\"x"] != float64(1) {
		t.Fatalf("unmatched key = %#v", decoded)
	}
}

func TestJSONPrefixStaysTextAndValueLiteralIsReplaced(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"names":["synthetic_field"],"values":["synthetic-token-value"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := policy.MaskText(`prefix {"synthetic_field":"synthetic-token-value"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "prefix") || !strings.Contains(got, "synthetic_field") || strings.Contains(got, "synthetic-token-value") {
		t.Fatalf("prefix text = %s", got)
	}
}

func TestJSONInsideStringAndRoundLimit(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"names":["synthetic_field"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := policy.MaskText(`{"note":"{\"synthetic_field\":\"ordinary-visible\"}","n":1}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "ordinary-visible") || strings.Contains(got, "synthetic_field") {
		t.Fatalf("nested string JSON leaked: %s", got)
	}
	if !strings.Contains(got, `"n":1`) {
		t.Fatalf("unmatched number changed: %s", got)
	}

	inner := `{"synthetic_field":1}`
	mid, err := json.Marshal(map[string]string{"b": inner})
	if err != nil {
		t.Fatal(err)
	}
	deep, err := json.Marshal(map[string]string{"a": string(mid)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.MaskText(string(deep), nil); err != ErrFailed {
		t.Fatalf("deep JSON error = %v, want ErrFailed", err)
	}
}

func TestDuplicateKeyAndCollisionFailWithoutSource(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"names":["a","b"]}`)
	if err != nil {
		t.Fatal(err)
	}
	secret := "synthetic-token-value"
	for _, raw := range []string{
		`{"a":1,"a":"` + secret + `"}`,
		`{"a":"` + secret + `","b":"ordinary-visible"}`,
	} {
		_, err := policy.MaskText(raw, nil)
		if err != ErrFailed || strings.Contains(err.Error(), secret) {
			t.Fatalf("MaskText error = %v for %s", err, raw)
		}
	}
}

func TestMaskBytesRewritesUTF8AndBinaryLiteral(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"values":["synthetic-token-value"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := policy.MaskBytes([]byte("xx synthetic-token-value yy"), nil)
	if err != nil || string(got) != "xx [masked] yy" {
		t.Fatalf("utf8 bytes = %q, %v", got, err)
	}
	raw := []byte{0xff}
	raw = append(raw, []byte("synthetic-token-value")...)
	raw = append(raw, 0xfe)
	got, err = policy.MaskBytes(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytesContains(got, []byte("synthetic-token-value")) {
		t.Fatalf("binary literal remains: %x", got)
	}
	if got[0] != 0xff || got[len(got)-1] != 0xfe {
		t.Fatalf("binary neighbors changed: %x", got)
	}
}

func TestInactiveMaskTextIsUnchanged(t *testing.T) {
	got, err := Disabled().MaskText("synthetic-token-value", nil)
	if err != nil || got != "synthetic-token-value" {
		t.Fatalf("inactive MaskText = %q, %v", got, err)
	}
}

func TestInvalidUTF8StillMatchesValuePattern(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"values":["synthetic-token-value"],"value_patterns":["sk_live_[0-9]+"]}`)
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte{0xff}, []byte("sk_live_4242 synthetic-token-value")...)
	got, err := policy.MaskBytes(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytesContains(got, []byte("sk_live_4242")) || bytesContains(got, []byte("synthetic-token-value")) {
		t.Fatalf("binary secrets remain: %x", got)
	}
	if got[0] != 0xff {
		t.Fatalf("leading byte changed: %x", got)
	}
}

func TestExpansionIsRejectedBeforeAHugeResult(t *testing.T) {
	placeholder := strings.Repeat("P", 64)
	policy, err := ParseJSON(`{"enabled":true,"placeholder":"` + placeholder + `","values":["a"]}`)
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 0, 40000)
	for i := 0; i < 20000; i++ {
		raw = append(raw, 0xff, 'a')
	}
	if _, err := policy.MaskBytes(raw, nil); err != ErrFailed {
		t.Fatalf("expanded binary error = %v, want ErrFailed", err)
	}
}

func TestNumericAndBoolValuesNullTheWholeScalar(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"values":["1.2300","true"]}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := policy.MaskText(`{"drop":1.2300,"keep":2.50,"on":true,"off":false}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "1.2300") || strings.Contains(got, "true") {
		t.Fatalf("matched scalars remain: %s", got)
	}
	if !strings.Contains(got, "2.50") || !strings.Contains(got, "false") {
		t.Fatalf("unmatched scalars changed: %s", got)
	}
}

func TestSharedBudgetCapsCombinedOutput(t *testing.T) {
	policy, err := ParseJSON(`{"enabled":true,"values":["zzzz"]}`)
	if err != nil {
		t.Fatal(err)
	}
	budget := &Budget{}
	chunk := strings.Repeat("b", 600000)
	if _, err := policy.MaskText(chunk, budget); err != nil {
		t.Fatal(err)
	}
	if _, err := policy.MaskText(chunk, budget); err != ErrFailed {
		t.Fatalf("second chunk error = %v, want ErrFailed", err)
	}
}

func bytesContains(haystack, needle []byte) bool {
	return len(needle) == 0 || strings.Contains(string(haystack), string(needle))
}
