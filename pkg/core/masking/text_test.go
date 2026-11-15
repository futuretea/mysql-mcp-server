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
	got, err := policy.MaskText(original)
	if err != nil || got != original {
		t.Fatalf("unmatched JSON = %q, %v", got, err)
	}

	got, err = policy.MaskText("{\"syn\\u0074hetic_field\":\"ordinary-visible\",\"keep\\u0022x\":1}")
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
	got, err := policy.MaskText(`prefix {"synthetic_field":"synthetic-token-value"}`)
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
	got, err := policy.MaskText(`{"note":"{\"synthetic_field\":\"ordinary-visible\"}","n":1}`)
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
	if _, err := policy.MaskText(string(deep)); err != ErrFailed {
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
		_, err := policy.MaskText(raw)
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
	got, err := policy.MaskBytes([]byte("xx synthetic-token-value yy"))
	if err != nil || string(got) != "xx [masked] yy" {
		t.Fatalf("utf8 bytes = %q, %v", got, err)
	}
	raw := []byte{0xff}
	raw = append(raw, []byte("synthetic-token-value")...)
	raw = append(raw, 0xfe)
	got, err = policy.MaskBytes(raw)
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
	got, err := Disabled().MaskText("synthetic-token-value")
	if err != nil || got != "synthetic-token-value" {
		t.Fatalf("inactive MaskText = %q, %v", got, err)
	}
}

func bytesContains(haystack, needle []byte) bool {
	return len(needle) == 0 || strings.Contains(string(haystack), string(needle))
}
