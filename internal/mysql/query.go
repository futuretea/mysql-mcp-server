package mysql

import (
	"errors"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

var errQueryRejected = errors.New("query rejected")

var forbiddenFunctionNames = map[string]struct{}{
	"last_insert_id": {},
	"load_file":      {},
	"uuid_short":     {},
}

// ValidateQuery accepts one non-locking read query and rejects all other SQL.
// It uses the MySQL grammar AST instead of a lexical allowlist.
func ValidateQuery(query string) error {
	if strings.TrimSpace(query) == "" {
		return errQueryRejected
	}

	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return errQueryRejected
	}
	statement, err := parser.Parse(query)
	if err != nil || !isReadStatement(statement) {
		return errQueryRejected
	}

	err = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		switch value := node.(type) {
		case *sqlparser.Select:
			if value.Into != nil || value.Lock != sqlparser.NoLock {
				return false, errQueryRejected
			}
		case *sqlparser.Union:
			if value.Into != nil || value.Lock != sqlparser.NoLock {
				return false, errQueryRejected
			}
		case *sqlparser.AssignmentExpr, *sqlparser.LockingFunc:
			return false, errQueryRejected
		case *sqlparser.FuncExpr:
			if _, forbidden := forbiddenFunctionNames[value.Name.Lowered()]; forbidden {
				return false, errQueryRejected
			}
		}
		return true, nil
	}, statement)
	if err != nil {
		return errQueryRejected
	}
	return nil
}

func isReadStatement(statement sqlparser.Statement) bool {
	switch statement.(type) {
	case *sqlparser.Select, *sqlparser.Union:
		return true
	default:
		return false
	}
}
