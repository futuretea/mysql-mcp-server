// Package masking removes configured field names and values from text before
// a MySQL tool result is encoded. Other database servers can copy this package.
// It does not detect every private value, compressed payload, or encoding.
//
// A policy is inactive unless enabled is true and at least one rule is set.
// Absent configuration is inactive. MYSQL_MCP_MASKING_JSON, when the variable
// is present, replaces the YAML masking object. An empty value is invalid.
// The environment variable being unset leaves the YAML object in place.
//
// Name patterns are matched against the whole name. Value literals and value
// patterns are substring matches on the original text, applied once. The
// replacement is not searched again. A one-character literal is valid and can
// match inside ordinary words. A pattern is rejected when any alternative can
// match without consuming input, including a word-boundary branch.
//
// Pass one *Budget to every MaskText, MaskBytes, and MatchScalar call that
// belongs to the same result. A nil budget caps that call alone. Output size
// is checked before the replaced text is built. Span lists are capped the
// same way.
//
// MatchScalar reports a hit on the original numeric or bool text. The caller
// replaces the whole scalar with null and does not edit inside the number.
// An unmatched number keeps its original literal, including trailing zeros.
//
// The guarantee is narrow. Original configured data is removed from covered
// MySQL representations: names, comments, defaults, column database and table
// identifiers, utf8 text, JSON objects and arrays stored as that text, JSON
// numbers and bools, integer and float cells, and standard-base64 cells after
// decode. Value patterns also run on ASCII subsequences inside invalid UTF-8.
// The placeholder and the fixed public errors are not scanned. A short
// literal that also occurs inside the placeholder or a fixed error can still
// be seen there. schema_version and the tool name are server constants and
// are not treated as stored data.
//
// A JSON null after masking can be a real SQL NULL or a redacted non-string.
// Types and nullability in the JSON can change when a rule matches.
package masking
