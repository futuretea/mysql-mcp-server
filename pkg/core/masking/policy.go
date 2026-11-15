package masking

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"regexp/syntax"
	"strings"
)

const (
	// InvalidConfigMessage is the only text returned for a bad masking document.
	InvalidConfigMessage = "invalid masking config"
	defaultPlaceholder   = "[masked]"
	maxRules             = 32
	maxNameBytes         = 128
	maxLiteralBytes      = 256
	maxPatternBytes      = 128
	maxInputBytes        = 1 << 20
	maxNodes             = 10000
	maxSpans             = 4096
)

// Budget counts nodes and output bytes for one tool result. Pass the same
// pointer to every MaskText, MaskBytes, and MatchScalar call in that result.
// A nil budget applies the same caps to that call alone.
type Budget struct {
	nodes int
	bytes int
}

func (b *Budget) add(nodes, size int) error {
	if b == nil {
		return nil
	}
	if nodes < 0 || size < 0 || b.nodes > maxNodes-nodes || b.bytes > maxInputBytes-size {
		return ErrFailed
	}
	b.nodes += nodes
	b.bytes += size
	return nil
}

// ErrFailed means a result cannot be returned without exposing covered data
// or exceeding a bound. The error text is not the source value.
var ErrFailed = errors.New("masking failed")

// Policy is the parsed masking document. The zero policy is inactive.
type Policy struct {
	enabled       bool
	placeholder   string
	names         map[string]struct{}
	namePatterns  []*regexp.Regexp
	values        []string
	valuePatterns []*regexp.Regexp
}

// Disabled is the inactive policy used when masking configuration is absent.
func Disabled() Policy {
	return Policy{placeholder: defaultPlaceholder}
}

// Enabled reports whether rules are applied.
func (p Policy) Enabled() bool { return p.enabled }

// Placeholder is the fixed string written in place of a matched string.
func (p Policy) Placeholder() string {
	if p.placeholder == "" {
		return defaultPlaceholder
	}
	return p.placeholder
}

// MatchName reports whether a structural name is selected by a name rule.
func (p Policy) MatchName(name string) bool {
	if !p.enabled {
		return false
	}
	if _, ok := p.names[name]; ok {
		return true
	}
	for _, pattern := range p.namePatterns {
		if pattern.MatchString(name) {
			return true
		}
	}
	return false
}

// ParseJSON parses MYSQL_MCP_MASKING_JSON. A present empty value is invalid.
func ParseJSON(raw string) (Policy, error) {
	if strings.TrimSpace(raw) == "" {
		return Policy{}, invalidConfig()
	}
	return parseBytes([]byte(raw))
}

// ParseRaw parses a YAML-decoded masking value. Nil means inactive.
func ParseRaw(value any) (Policy, error) {
	if value == nil {
		return Disabled(), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return Policy{}, invalidConfig()
	}
	return parseBytes(raw)
}

func invalidConfig() error {
	return errors.New(InvalidConfigMessage)
}

type document struct {
	Enabled       *bool    `json:"enabled"`
	Placeholder   *string  `json:"placeholder"`
	Names         []string `json:"names"`
	NamePatterns  []string `json:"name_patterns"`
	Values        []string `json:"values"`
	ValuePatterns []string `json:"value_patterns"`
}

func parseBytes(raw []byte) (Policy, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var doc document
	if err := decoder.Decode(&doc); err != nil {
		return Policy{}, invalidConfig()
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Policy{}, invalidConfig()
	}
	return compile(doc)
}

func compile(doc document) (Policy, error) {
	placeholder := defaultPlaceholder
	if doc.Placeholder != nil {
		placeholder = *doc.Placeholder
	}
	if placeholder == "" || len(placeholder) > 64 {
		return Policy{}, invalidConfig()
	}
	names, err := exactList(doc.Names, maxNameBytes)
	if err != nil {
		return Policy{}, err
	}
	values, err := exactList(doc.Values, maxLiteralBytes)
	if err != nil {
		return Policy{}, err
	}
	namePatterns, err := compilePatterns(doc.NamePatterns, true)
	if err != nil {
		return Policy{}, err
	}
	valuePatterns, err := compilePatterns(doc.ValuePatterns, false)
	if err != nil {
		return Policy{}, err
	}
	enabled := doc.Enabled != nil && *doc.Enabled
	if enabled && len(names) == 0 && len(values) == 0 && len(namePatterns) == 0 && len(valuePatterns) == 0 {
		return Policy{}, invalidConfig()
	}
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[name] = struct{}{}
	}
	return Policy{
		enabled:       enabled,
		placeholder:   placeholder,
		names:         nameSet,
		namePatterns:  namePatterns,
		values:        values,
		valuePatterns: valuePatterns,
	}, nil
}

func exactList(values []string, maxBytes int) ([]string, error) {
	if len(values) > maxRules {
		return nil, invalidConfig()
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || len(value) > maxBytes {
			return nil, invalidConfig()
		}
		out = append(out, value)
	}
	return out, nil
}

func compilePatterns(patterns []string, names bool) ([]*regexp.Regexp, error) {
	if len(patterns) > maxRules {
		return nil, invalidConfig()
	}
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern == "" || len(pattern) > maxPatternBytes {
			return nil, invalidConfig()
		}
		expr := pattern
		if names {
			expr = `\A(?:` + pattern + `)\z`
		}
		re, err := regexp.Compile(expr)
		if err != nil || re.MatchString("") || !consumesInput(expr) {
			return nil, invalidConfig()
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// consumesInput reports whether every successful alternative consumes at least
// one rune. A branch that can match empty, including a word boundary, is rejected.
func consumesInput(expr string) bool {
	parsed, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return false
	}
	parsed = parsed.Simplify()
	return minMatch(parsed) > 0
}

func minMatch(re *syntax.Regexp) int {
	switch re.Op {
	case syntax.OpNoMatch:
		return 1
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return 0
	case syntax.OpLiteral:
		return len(re.Rune)
	case syntax.OpCharClass, syntax.OpAnyCharNotNL, syntax.OpAnyChar:
		return 1
	case syntax.OpCapture, syntax.OpPlus:
		if len(re.Sub) == 0 {
			return 0
		}
		return minMatch(re.Sub[0])
	case syntax.OpStar, syntax.OpQuest:
		return 0
	case syntax.OpRepeat:
		if len(re.Sub) == 0 || re.Min == 0 {
			return 0
		}
		inner := minMatch(re.Sub[0])
		if inner == 0 || re.Min > maxInputBytes/inner {
			return 0
		}
		return re.Min * inner
	case syntax.OpConcat:
		total := 0
		for _, sub := range re.Sub {
			width := minMatch(sub)
			if width > maxInputBytes-total {
				return maxInputBytes
			}
			total += width
		}
		return total
	case syntax.OpAlternate:
		if len(re.Sub) == 0 {
			return 0
		}
		width := minMatch(re.Sub[0])
		for _, sub := range re.Sub[1:] {
			next := minMatch(sub)
			if next < width {
				width = next
			}
		}
		return width
	default:
		return 0
	}
}
