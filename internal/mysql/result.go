package mysql

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// Cell is the stable JSON representation of a non-null query result value.
type Cell struct {
	Encoding string `json:"encoding"`
	Value    string `json:"value"`
}

// EncodeValue converts database/sql driver values into the documented result
// representation. Values outside the explicit contract are rejected.
func EncodeValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return encodeText([]byte(typed)), nil
	case []byte:
		return encodeText(typed), nil
	case int64:
		return Cell{Encoding: "integer", Value: strconv.FormatInt(typed, 10)}, nil
	case uint64:
		return Cell{Encoding: "integer", Value: strconv.FormatUint(typed, 10)}, nil
	case float32:
		return Cell{Encoding: "float", Value: strconv.FormatFloat(float64(typed), 'g', -1, 32)}, nil
	case float64:
		return Cell{Encoding: "float", Value: strconv.FormatFloat(typed, 'g', -1, 64)}, nil
	default:
		return nil, fmt.Errorf("unsupported driver value type %T", value)
	}
}

func encodeText(value []byte) Cell {
	if utf8.Valid(value) {
		return Cell{Encoding: "utf8", Value: string(value)}
	}
	return Cell{Encoding: "base64", Value: base64.StdEncoding.EncodeToString(value)}
}
