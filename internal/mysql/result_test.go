package mysql

import (
	"encoding/json"
	"testing"
)

func TestEncodeValue(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{name: "null", input: nil, want: "null"},
		{name: "utf8 string", input: "hello", want: `{"encoding":"utf8","value":"hello"}`},
		{name: "utf8 bytes", input: []byte("hello"), want: `{"encoding":"utf8","value":"hello"}`},
		{name: "binary bytes", input: []byte{0xff}, want: `{"encoding":"base64","value":"/w=="}`},
		{name: "signed integer", input: int64(-7), want: `{"encoding":"integer","value":"-7"}`},
		{name: "unsigned integer", input: uint64(18446744073709551615), want: `{"encoding":"integer","value":"18446744073709551615"}`},
		{name: "float32", input: float32(1.5), want: `{"encoding":"float","value":"1.5"}`},
		{name: "float64", input: 1.25, want: `{"encoding":"float","value":"1.25"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeValue(tt.input)
			if err != nil {
				t.Fatalf("EncodeValue(%T) error = %v", tt.input, err)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}
			if string(raw) != tt.want {
				t.Fatalf("EncodeValue(%T) = %s, want %s", tt.input, raw, tt.want)
			}
		})
	}
}

func TestEncodeValueRejectsUnknownDriverValue(t *testing.T) {
	if _, err := EncodeValue(struct{}{}); err == nil {
		t.Fatal("EncodeValue(struct{}{}) error = nil, want error")
	}
}
