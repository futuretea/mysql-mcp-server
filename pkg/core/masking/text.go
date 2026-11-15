package masking

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxInputBytes   = 1 << 20
	maxDecodedBytes = 1 << 20
	maxNodes        = 10000
	maxDepth        = 32
	maxJSONRounds   = 2
)

type span struct{ start, end int }

// MaskText applies value rules and, when the whole string is a JSON object or
// array, field rules. An inactive policy returns the input unchanged.
func (p Policy) MaskText(value string) (string, error) {
	if !p.enabled {
		return value, nil
	}
	nodes := 0
	out, _, err := p.maskString(value, 0, &nodes)
	return out, err
}

// MaskBytes decodes a known byte payload. Valid UTF-8 uses MaskText. Other
// bytes are searched for configured literals only. Regex is not applied to
// invalid UTF-8. The result is the bytes to encode again.
func (p Policy) MaskBytes(value []byte) ([]byte, error) {
	if !p.enabled {
		return append([]byte(nil), value...), nil
	}
	if len(value) > maxDecodedBytes {
		return nil, ErrFailed
	}
	if utf8.Valid(value) {
		text, err := p.MaskText(string(value))
		if err != nil {
			return nil, err
		}
		return []byte(text), nil
	}
	return p.replaceByteLiterals(value), nil
}

func (p Policy) maskString(value string, rounds int, nodes *int) (string, bool, error) {
	if len(value) > maxInputBytes {
		return "", false, ErrFailed
	}
	if rounds < maxJSONRounds {
		if out, ok, err := p.maskJSONDocument(value, rounds, nodes); err != nil || ok {
			return out, out != value, err
		}
	} else if _, ok, err := parseStructural(value); err != nil {
		return "", false, err
	} else if ok {
		return "", false, ErrFailed
	}
	out := p.replaceSpans(value)
	if len(out) > maxInputBytes {
		return "", false, ErrFailed
	}
	return out, out != value, nil
}

func parseStructural(value string) (any, bool, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, false, nil
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var skip any
	if err := decoder.Decode(&skip); err != nil {
		if isJSONSyntax(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if decoder.More() {
		return nil, false, nil
	}
	return skip, true, nil
}

func (p Policy) maskJSONDocument(value string, rounds int, nodes *int) (string, bool, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return "", false, nil
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	node, changed, err := p.maskNode(decoder, 1, rounds+1, nodes)
	if err != nil {
		if isJSONSyntax(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if decoder.More() {
		return "", false, nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errorsIsEOF(err) {
		return "", false, nil
	}
	if !changed {
		return value, true, nil
	}
	encoded, err := marshalNode(node)
	if err != nil || len(encoded) > maxInputBytes {
		return "", false, ErrFailed
	}
	return string(encoded), true, nil
}

func errorsIsEOF(err error) bool {
	return errors.Is(err, io.EOF)
}

func isJSONSyntax(err error) bool {
	if err == nil || errors.Is(err, ErrFailed) {
		return false
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return true
	}
	var unmarshal *json.UnmarshalTypeError
	if errors.As(err, &unmarshal) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}

func (p Policy) maskNode(decoder *json.Decoder, depth, rounds int, nodes *int) (node, bool, error) {
	*nodes++
	if *nodes > maxNodes || depth > maxDepth {
		return node{}, false, ErrFailed
	}
	token, err := decoder.Token()
	if err != nil {
		return node{}, false, err
	}
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			return p.maskObject(decoder, depth, rounds, nodes)
		case '[':
			return p.maskArray(decoder, depth, rounds, nodes)
		default:
			return node{}, false, ErrFailed
		}
	case string:
		text, changed, err := p.maskString(typed, rounds, nodes)
		if err != nil {
			return node{}, false, err
		}
		return node{kind: "string", text: text}, changed, nil
	case nil:
		return node{kind: "null"}, false, nil
	case bool:
		return node{kind: "bool", flag: typed}, false, nil
	case json.Number:
		return node{kind: "number", text: typed.String()}, false, nil
	case float64:
		raw, err := json.Marshal(typed)
		if err != nil {
			return node{}, false, ErrFailed
		}
		return node{kind: "number", text: string(raw)}, false, nil
	default:
		return node{}, false, ErrFailed
	}
}

func (p Policy) maskObject(decoder *json.Decoder, depth, rounds int, nodes *int) (node, bool, error) {
	fields := make([]field, 0)
	seen := make(map[string]struct{})
	changed := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return node{}, false, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return node{}, false, ErrFailed
		}
		if _, dup := seen[key]; dup {
			return node{}, false, ErrFailed
		}
		seen[key] = struct{}{}
		value, valueChanged, err := p.maskNode(decoder, depth+1, rounds, nodes)
		if err != nil {
			return node{}, false, err
		}
		keyOut := key
		if p.MatchName(key) {
			keyOut = p.Placeholder()
			value = wholeValue(value, p.Placeholder())
			changed = true
		} else {
			replaced := p.replaceSpans(key)
			if replaced != key {
				keyOut = replaced
				changed = true
			}
			changed = changed || valueChanged
		}
		fields = append(fields, field{key: keyOut, value: value})
	}
	end, err := decoder.Token()
	if err != nil {
		return node{}, false, err
	}
	delim, ok := end.(json.Delim)
	if !ok || delim != '}' {
		return node{}, false, ErrFailed
	}
	if err := rejectCollisions(fields); err != nil {
		return node{}, false, err
	}
	return node{kind: "object", fields: fields}, changed, nil
}

func (p Policy) maskArray(decoder *json.Decoder, depth, rounds int, nodes *int) (node, bool, error) {
	items := make([]node, 0)
	changed := false
	for decoder.More() {
		item, itemChanged, err := p.maskNode(decoder, depth+1, rounds, nodes)
		if err != nil {
			return node{}, false, err
		}
		changed = changed || itemChanged
		items = append(items, item)
	}
	end, err := decoder.Token()
	if err != nil {
		return node{}, false, err
	}
	delim, ok := end.(json.Delim)
	if !ok || delim != ']' {
		return node{}, false, ErrFailed
	}
	return node{kind: "array", items: items}, changed, nil
}

func wholeValue(value node, placeholder string) node {
	if value.kind == "null" {
		return value
	}
	if value.kind == "string" {
		return node{kind: "string", text: placeholder}
	}
	return node{kind: "null"}
}

func rejectCollisions(fields []field) error {
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if _, ok := seen[field.key]; ok {
			return ErrFailed
		}
		seen[field.key] = struct{}{}
	}
	return nil
}

type node struct {
	kind   string
	flag   bool
	text   string
	items  []node
	fields []field
}

type field struct {
	key   string
	value node
}

func marshalNode(value node) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeNode(&buf, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeNode(buf *bytes.Buffer, value node) error {
	switch value.kind {
	case "null":
		buf.WriteString("null")
	case "bool":
		if value.flag {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case "number":
		buf.WriteString(value.text)
	case "string":
		raw, err := json.Marshal(value.text)
		if err != nil {
			return ErrFailed
		}
		buf.Write(raw)
	case "array":
		buf.WriteByte('[')
		for i, item := range value.items {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeNode(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case "object":
		buf.WriteByte('{')
		for i, field := range value.fields {
			if i > 0 {
				buf.WriteByte(',')
			}
			raw, err := json.Marshal(field.key)
			if err != nil {
				return ErrFailed
			}
			buf.Write(raw)
			buf.WriteByte(':')
			if err := writeNode(buf, field.value); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return ErrFailed
	}
	return nil
}

func (p Policy) replaceSpans(value string) string {
	spans := p.collectSpans(value)
	if len(spans) == 0 {
		return value
	}
	var buf strings.Builder
	buf.Grow(len(value))
	last := 0
	for _, item := range spans {
		buf.WriteString(value[last:item.start])
		buf.WriteString(p.Placeholder())
		last = item.end
	}
	buf.WriteString(value[last:])
	return buf.String()
}

func (p Policy) collectSpans(value string) []span {
	found := make([]span, 0)
	literals := append([]string(nil), p.values...)
	sort.Slice(literals, func(i, j int) bool { return len(literals[i]) > len(literals[j]) })
	for _, literal := range literals {
		from := 0
		for from <= len(value)-len(literal) {
			index := strings.Index(value[from:], literal)
			if index < 0 {
				break
			}
			start := from + index
			found = append(found, span{start: start, end: start + len(literal)})
			from = start + len(literal)
		}
	}
	for _, pattern := range p.valuePatterns {
		for _, loc := range pattern.FindAllStringIndex(value, -1) {
			if loc[0] == loc[1] {
				continue
			}
			found = append(found, span{start: loc[0], end: loc[1]})
		}
	}
	return mergeSpans(found)
}

func mergeSpans(spans []span) []span {
	if len(spans) == 0 {
		return nil
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end > spans[j].end
		}
		return spans[i].start < spans[j].start
	})
	merged := []span{spans[0]}
	for _, item := range spans[1:] {
		last := &merged[len(merged)-1]
		if item.start <= last.end {
			if item.end > last.end {
				last.end = item.end
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func (p Policy) replaceByteLiterals(value []byte) []byte {
	type byteSpan struct{ start, end int }
	found := make([]byteSpan, 0)
	literals := append([]string(nil), p.values...)
	sort.Slice(literals, func(i, j int) bool { return len(literals[i]) > len(literals[j]) })
	for _, literal := range literals {
		needle := []byte(literal)
		from := 0
		for from <= len(value)-len(needle) {
			index := bytes.Index(value[from:], needle)
			if index < 0 {
				break
			}
			start := from + index
			found = append(found, byteSpan{start: start, end: start + len(needle)})
			from = start + len(needle)
		}
	}
	if len(found) == 0 {
		return append([]byte(nil), value...)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].start < found[j].start })
	merged := []byteSpan{found[0]}
	for _, item := range found[1:] {
		last := &merged[len(merged)-1]
		if item.start <= last.end {
			if item.end > last.end {
				last.end = item.end
			}
			continue
		}
		merged = append(merged, item)
	}
	placeholder := []byte(p.Placeholder())
	var buf bytes.Buffer
	last := 0
	for _, item := range merged {
		buf.Write(value[last:item.start])
		buf.Write(placeholder)
		last = item.end
	}
	buf.Write(value[last:])
	return buf.Bytes()
}
