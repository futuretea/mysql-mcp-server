package mysql

import (
	"context"
	"testing"
)

func TestValidateQuery(t *testing.T) {
	allowed := []string{
		"SELECT 1",
		"WITH recent AS (SELECT 1 AS id) SELECT id FROM recent",
		"SELECT 'GET_LOCK is text', /* INTO OUTFILE */ 1",
	}
	for _, sql := range allowed {
		t.Run("allows_"+sql, func(t *testing.T) {
			if err := ValidateQuery(sql); err != nil {
				t.Fatalf("ValidateQuery(%q) error = %v", sql, err)
			}
		})
	}

	rejected := []string{
		"INSERT INTO accounts VALUES (1)",
		"SELECT 1; SELECT 2",
		"SELECT 1 INTO OUTFILE 'result.txt'",
		"SELECT * FROM accounts FOR UPDATE",
		"SELECT * FROM accounts LOCK IN SHARE MODE",
		"SELECT @counter := 1",
		"SELECT GET_LOCK('x', 1)",
		"SELECT COALESCE(RELEASE_LOCK('x'), 0)",
		"SELECT RELEASE_ALL_LOCKS()",
		"SELECT LAST_INSERT_ID()",
		"SELECT LOAD_FILE('path')",
		"SELECT UUID_SHORT()",
	}
	for _, sql := range rejected {
		t.Run("rejects_"+sql, func(t *testing.T) {
			if err := ValidateQuery(sql); err == nil {
				t.Fatalf("ValidateQuery(%q) error = nil, want rejection", sql)
			}
		})
	}
}

func TestServiceQueryRejectsBlockedFunctionBeforeDatabaseAccess(t *testing.T) {
	service := &Service{}
	_, err := service.Query(context.Background(), "SELECT UUID_SHORT()")
	if code, ok := CodeOf(err); !ok || code != CodeQueryRejected {
		t.Fatalf("Query() error code = %q, %t; want %q", code, ok, CodeQueryRejected)
	}
}
