package masking

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
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
)

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
		if err != nil || re.MatchString("") {
			return nil, invalidConfig()
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}
