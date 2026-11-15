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
	maxDecodedBytes = 1 << 20
	maxDepth        = 32
	maxJSONRounds   = 2
)

type span struct{ start, end int }

// MaskText applies value rules and, when the whole string is a JSON object or
// array, field rules. An inactive policy returns the input unchanged.
func (p Policy) MaskText(value string, budget *Budget) (string, error) {
	if !p.enabled {
		return value, nil
	}
	if budget == nil {
		budget = &Budget{}
	}
	out, _, err := p.maskString(value, 0, budget)
	return out, err
}

// MaskBytes applies value rules to a known decoded payload, including an
// ASCII secret surrounded by invalid UTF-8. Valid UTF-8 also uses JSON rules.
// The returned bytes are what the caller encodes again.
func (p Policy) MaskBytes(value []byte, budget *Budget) ([]byte, error) {
	if !p.enabled {
		return append([]byte(nil), value...), nil
	}
	if budget == nil {
		budget = &Budget{}
	}
	if len(value) > maxDecodedBytes {
		return nil, ErrFailed
	}
	if utf8.Valid(value) {
		text, err := p.MaskText(string(value), budget)
		if err != nil {
			return nil, err
		}
		return []byte(text), nil
	}
	text, err := p.replaceSpans(string(value), budget)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// MatchScalar reports whether a value rule hits the original scalar text.
// A hit means the whole number or bool becomes null. The text is not edited.
func (p Policy) MatchScalar(original string, budget *Budget) (bool, error) {
	if !p.enabled {
		return false, nil
	}
	if budget == nil {
		budget = &Budget{}
	}
	if len(original) > maxInputBytes {
		return false, ErrFailed
	}
	spans, err := p.collectSpans(original)
	if err != nil {
		return false, err
	}
	if err := budget.add(1, len(original)); err != nil {
		return false, err
	}
	return len(spans) > 0, nil
}

func (p Policy) maskString(value string, rounds int, budget *Budget) (string, bool, error) {
	if len(value) > maxInputBytes {
		return "", false, ErrFailed
	}
	if rounds < maxJSONRounds {
		if out, ok, err := p.maskJSONDocument(value, rounds, budget); err != nil || ok {
			if err != nil {
				return "", false, err
			}
			if err := budget.add(1, len(out)); err != nil {
				return "", false, err
			}
			return out, out != value, nil
		}
	} else if _, ok, err := parseStructural(value); err != nil {
		return "", false, err
	} else if ok {
		return "", false, ErrFailed
	}
	out, err := p.replaceSpans(value, budget)
	return out, out != value, err
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

func (p Policy) maskJSONDocument(value string, rounds int, budget *Budget) (string, bool, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return "", false, nil
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	node, changed, err := p.maskNode(decoder, 1, rounds+1, budget)
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

func (p Policy) maskNode(decoder *json.Decoder, depth, rounds int, budget *Budget) (node, bool, error) {
	if err := budget.add(1, 0); err != nil || depth > maxDepth {
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
			return p.maskObject(decoder, depth, rounds, budget)
		case '[':
			return p.maskArray(decoder, depth, rounds, budget)
		default:
			return node{}, false, ErrFailed
		}
	case string:
		text, changed, err := p.maskString(typed, rounds, budget)
		if err != nil {
			return node{}, false, err
		}
		return node{kind: "string", text: text}, changed, nil
	case nil:
		return node{kind: "null"}, false, nil
	case json.Number:
		return p.maskNumber(typed.String(), budget)
	case float64:
		raw, err := json.Marshal(typed)
		if err != nil {
			return node{}, false, ErrFailed
		}
		return p.maskNumber(string(raw), budget)
	case bool:
		text := "false"
		if typed {
			text = "true"
		}
		hit, err := p.MatchScalar(text, budget)
		if err != nil {
			return node{}, false, err
		}
		if hit {
			return node{kind: "null"}, true, nil
		}
		return node{kind: "bool", flag: typed}, false, nil
	default:
		return node{}, false, ErrFailed
	}
}

func (p Policy) maskNumber(original string, budget *Budget) (node, bool, error) {
	hit, err := p.MatchScalar(original, budget)
	if err != nil {
		return node{}, false, err
	}
	if hit {
		return node{kind: "null"}, true, nil
	}
	return node{kind: "number", text: original}, false, nil
}

func (p Policy) maskObject(decoder *json.Decoder, depth, rounds int, budget *Budget) (node, bool, error) {
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
		value, valueChanged, err := p.maskNode(decoder, depth+1, rounds, budget)
		if err != nil {
			return node{}, false, err
		}
		keyOut := key
		if p.MatchName(key) {
			keyOut = p.Placeholder()
			value = wholeValue(value, p.Placeholder())
			changed = true
		} else {
			replaced, err := p.replaceSpans(key, budget)
			if err != nil {
				return node{}, false, err
			}
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

func (p Policy) maskArray(decoder *json.Decoder, depth, rounds int, budget *Budget) (node, bool, error) {
	items := make([]node, 0)
	changed := false
	for decoder.More() {
		item, itemChanged, err := p.maskNode(decoder, depth+1, rounds, budget)
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

func (p Policy) replaceSpans(value string, budget *Budget) (string, error) {
	if len(value) > maxInputBytes {
		return "", ErrFailed
	}
	spans, err := p.collectSpans(value)
	if err != nil {
		return "", err
	}
	size, err := outputSize(len(value), spans, len(p.Placeholder()))
	if err != nil {
		return "", err
	}
	if err := budget.add(1, size); err != nil {
		return "", err
	}
	if len(spans) == 0 {
		return value, nil
	}
	var buf strings.Builder
	buf.Grow(size)
	last := 0
	for _, item := range spans {
		buf.WriteString(value[last:item.start])
		buf.WriteString(p.Placeholder())
		last = item.end
	}
	buf.WriteString(value[last:])
	return buf.String(), nil
}

func (p Policy) collectSpans(value string) ([]span, error) {
	found := make([]span, 0)
	add := func(start, end int) error {
		if end <= start {
			return ErrFailed
		}
		if len(found) == maxSpans {
			return ErrFailed
		}
		found = append(found, span{start: start, end: end})
		return nil
	}
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
			if err := add(start, start+len(literal)); err != nil {
				return nil, err
			}
			from = start + len(literal)
		}
	}
	for _, pattern := range p.valuePatterns {
		for _, loc := range pattern.FindAllStringIndex(value, -1) {
			if err := add(loc[0], loc[1]); err != nil {
				return nil, err
			}
		}
	}
	return mergeSpans(found), nil
}

func outputSize(input int, spans []span, placeholder int) (int, error) {
	size := input
	for _, item := range spans {
		width := item.end - item.start
		if width <= 0 || item.start < 0 || item.end > input {
			return 0, ErrFailed
		}
		if size < width {
			return 0, ErrFailed
		}
		size -= width
		if placeholder > maxInputBytes-size {
			return 0, ErrFailed
		}
		size += placeholder
	}
	if size > maxInputBytes {
		return 0, ErrFailed
	}
	return size, nil
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
