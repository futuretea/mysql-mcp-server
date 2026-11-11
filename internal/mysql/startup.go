package mysql

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const startupRoleOptOutMessage = "database service role is not permitted; allow_unsafe_database_role is set, so the startup role check is skipped"

// showCurrentGrantsSQL is the initial probe. MySQL includes mandatory roles
// only when SHOW GRANTS has no FOR clause. SHOW GRANTS FOR CURRENT_USER()
// hides them, which would let a mandatory role carry FILE or SUPER past startup.
const showCurrentGrantsSQL = "SHOW GRANTS"

const maxRoleExpansions = 8

var startupTimeout = 5 * time.Second

var unsafePrivilegeNames = []string{
	"SUPER",
	"FILE",
	"PROCESS",
	"SHUTDOWN",
	"RELOAD",
	"CREATE USER",
	"CREATE ROLE",
	"DROP ROLE",
	"EXECUTE",
	"CREATE TABLESPACE",
	"PROXY",
	"REPLICATION CLIENT",
	"REPLICATION SLAVE",
	"ALLOW_NONEXISTENT_DEFINER",
	"APPLICATION_PASSWORD_ADMIN",
	"AUDIT_ABORT_EXEMPT",
	"AUDIT_ADMIN",
	"AUTHENTICATION_POLICY_ADMIN",
	"BACKUP_ADMIN",
	"BINLOG_ADMIN",
	"BINLOG_ENCRYPTION_ADMIN",
	"CLONE_ADMIN",
	"CONNECTION_ADMIN",
	"ENCRYPTION_KEY_ADMIN",
	"FIREWALL_ADMIN",
	"FIREWALL_EXEMPT",
	"FIREWALL_USER",
	"FLUSH_OPTIMIZER_COSTS",
	"FLUSH_PRIVILEGES",
	"FLUSH_STATUS",
	"FLUSH_TABLES",
	"FLUSH_USER_RESOURCES",
	"GROUP_REPLICATION_ADMIN",
	"GROUP_REPLICATION_STREAM",
	"INNODB_REDO_LOG_ARCHIVE",
	"INNODB_REDO_LOG_ENABLE",
	"MASKING_DICTIONARIES_ADMIN",
	"OPTIMIZE_LOCAL_TABLE",
	"PASSWORDLESS_USER_ADMIN",
	"PERSIST_RO_VARIABLES_ADMIN",
	"REPLICATION_APPLIER",
	"REPLICATION_SLAVE_ADMIN",
	"RESOURCE_GROUP_ADMIN",
	"RESOURCE_GROUP_USER",
	"ROLE_ADMIN",
	"SENSITIVE_VARIABLES_OBSERVER",
	"SERVICE_CONNECTION_ADMIN",
	"SESSION_VARIABLES_ADMIN",
	"SET_ANY_DEFINER",
	"SET_USER_ID",
	"SHOW_ROUTINE",
	"SKIP_QUERY_REWRITE",
	"SYSTEM_USER",
	"SYSTEM_VARIABLES_ADMIN",
	"TABLE_ENCRYPTION_ADMIN",
	"TELEMETRY_LOG_ADMIN",
	"TP_CONNECTION_ADMIN",
	"TRANSACTION_GTID_TAG",
	"VERSION_TOKEN_ADMIN",
	"XA_RECOVER_ADMIN",
}

var unsafePrivileges = func() map[string]struct{} {
	privileges := make(map[string]struct{}, len(unsafePrivilegeNames))
	for _, name := range unsafePrivilegeNames {
		privileges[name] = struct{}{}
	}
	return privileges
}()

func (s *Service) verifyStartup(ctx context.Context, allowUnsafeDatabaseRole bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return databaseError("startup.begin", err)
	}
	defer func() { _ = tx.Rollback() }()

	unsafe, err := serviceAccountUnsafe(ctx, tx)
	if err != nil {
		return err
	}
	if !unsafe {
		return nil
	}
	if allowUnsafeDatabaseRole {
		log.Warn().Msg(startupRoleOptOutMessage)
		return nil
	}
	return codeError(CodeUnsafeServiceRole)
}

func serviceAccountUnsafe(ctx context.Context, tx *sql.Tx) (bool, error) {
	grants, err := readGrantStatements(ctx, tx, showCurrentGrantsSQL)
	if err != nil {
		return false, err
	}
	unsafe, roles, err := classifyGrants(grants)
	if err != nil {
		return false, err
	}

	seen := make(map[string]struct{}, len(roles))
	pending := make([]string, 0, len(roles))
	for _, role := range roles {
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		pending = append(pending, role)
	}

	for round := 0; len(pending) > 0; round++ {
		if round >= maxRoleExpansions {
			return false, codeError(CodeDatabase)
		}
		query, err := showGrantsUsing(seen)
		if err != nil {
			return false, err
		}
		expanded, err := readGrantStatements(ctx, tx, query)
		if err != nil {
			return false, err
		}
		expandedUnsafe, expandedRoles, err := classifyGrants(expanded)
		if err != nil {
			return false, err
		}
		unsafe = unsafe || expandedUnsafe
		pending = pending[:0]
		for _, role := range expandedRoles {
			if _, ok := seen[role]; ok {
				continue
			}
			seen[role] = struct{}{}
			pending = append(pending, role)
		}
	}
	return unsafe, nil
}

func readGrantStatements(ctx context.Context, tx *sql.Tx, query string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, databaseError("startup.grants", err)
	}
	defer func() { _ = rows.Close() }()

	grants := make([]string, 0)
	for rows.Next() {
		var grant string
		if err := rows.Scan(&grant); err != nil {
			return nil, databaseError("startup.grants.scan", err)
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("startup.grants.rows", err)
	}
	if len(grants) == 0 {
		return nil, codeError(CodeDatabase)
	}
	return grants, nil
}

func showGrantsUsing(roles map[string]struct{}) (string, error) {
	ordered := make([]string, 0, len(roles))
	for role := range roles {
		if !validRoleSpec(role) {
			return "", codeError(CodeDatabase)
		}
		ordered = append(ordered, role)
	}
	sort.Strings(ordered)
	// USING is valid only on SHOW GRANTS FOR user. Mandatory roles discovered
	// by the initial SHOW GRANTS probe are included in this list.
	return "SHOW GRANTS FOR CURRENT_USER() USING " + strings.Join(ordered, ", "), nil
}

func classifyGrants(grants []string) (bool, []string, error) {
	unsafe := false
	var roles []string
	for _, grant := range grants {
		grantUnsafe, grantRoles, err := classifyGrant(grant)
		if err != nil {
			return false, nil, err
		}
		unsafe = unsafe || grantUnsafe
		roles = append(roles, grantRoles...)
	}
	return unsafe, roles, nil
}

func classifyGrant(statement string) (bool, []string, error) {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return false, nil, codeError(CodeDatabase)
	}
	if strings.HasPrefix(strings.ToUpper(statement), "REVOKE ") {
		return false, nil, nil
	}
	body, ok := stripGrantPrefix(statement)
	if !ok {
		return false, nil, codeError(CodeDatabase)
	}
	onAt := indexKeywordOutsideQuotes(body, " ON ")
	toAt := indexKeywordOutsideQuotes(body, " TO ")
	if onAt >= 0 && toAt > onAt {
		names, parsed := splitPrivileges(body[:onAt])
		scope := strings.TrimSpace(body[onAt+len(" ON ") : toAt])
		if !parsed || scope == "" {
			return false, nil, codeError(CodeDatabase)
		}
		return privilegesUnsafe(names, scope), nil, nil
	}
	if toAt >= 0 && onAt < 0 {
		roles, parsed := splitRoleSpecs(body[:toAt])
		if !parsed {
			return false, nil, codeError(CodeDatabase)
		}
		return false, roles, nil
	}
	return false, nil, codeError(CodeDatabase)
}

func stripGrantPrefix(statement string) (string, bool) {
	if len(statement) < len("GRANT ") || !strings.EqualFold(statement[:len("GRANT")], "GRANT") {
		return "", false
	}
	body := strings.TrimSpace(statement[len("GRANT"):])
	if body == "" {
		return "", false
	}
	return body, true
}

func indexKeywordOutsideQuotes(value, keyword string) int {
	if keyword == "" || len(value) < len(keyword) {
		return -1
	}
	// Compare the ASCII keyword against the original bytes. Uppercasing the
	// whole statement is unsafe: letters such as U+0131 grow or shrink under
	// Unicode case mapping and the resulting string no longer shares indexes
	// with value, which panics or skips the real ON/TO boundary.
	var quote byte
	for i := 0; i < len(value); {
		if quote == 0 && i+len(keyword) <= len(value) && asciiEqualFold(value[i:i+len(keyword)], keyword) {
			return i
		}
		next, nextQuote := advanceQuoted(value, i, quote)
		if next <= i {
			return -1
		}
		i, quote = next, nextQuote
	}
	return -1
}

func identQuote(ch byte) bool {
	return ch == '`' || ch == '"' || ch == '\''
}

func advanceQuoted(value string, i int, quote byte) (int, byte) {
	if i >= len(value) {
		return i, quote
	}
	ch := value[i]
	if quote != 0 {
		if ch == quote {
			if i+1 < len(value) && value[i+1] == quote {
				return i + 2, quote
			}
			return i + 1, 0
		}
		return i + 1, quote
	}
	if identQuote(ch) {
		return i + 1, ch
	}
	return i + 1, 0
}

func asciiEqualFold(value, keyword string) bool {
	if len(value) != len(keyword) {
		return false
	}
	for i := 0; i < len(value); i++ {
		a := value[i]
		b := keyword[i]
		if a >= 'a' && a <= 'z' {
			a -= 'a' - 'A'
		}
		if b >= 'a' && b <= 'z' {
			b -= 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func privilegesUnsafe(names []string, scope string) bool {
	schema, schemaWide, global, scopeOK := scopeShape(scope)
	if scopeOK && strings.EqualFold(schema, "mysql") {
		return true
	}
	for _, name := range names {
		base := privilegeBase(name)
		switch base {
		case "ALL", "ALL PRIVILEGES":
			if !scopeOK || global || schemaWide {
				return true
			}
		case "SELECT":
			if !scopeOK || global {
				return true
			}
		default:
			if _, ok := unsafePrivileges[base]; ok {
				return true
			}
		}
	}
	return false
}

func privilegeBase(name string) string {
	name = strings.TrimSpace(name)
	if paren := strings.IndexByte(name, '('); paren >= 0 {
		name = strings.TrimSpace(name[:paren])
	}
	return name
}

func scopeShape(scope string) (schema string, schemaWide bool, global bool, ok bool) {
	parts, splitOK := splitOutsideQuotes(strings.TrimSpace(scope), '.')
	if !splitOK || len(parts) != 2 {
		return "", false, false, false
	}
	left, leftOK := unquoteIdent(parts[0])
	right, rightOK := unquoteIdent(parts[1])
	if !leftOK || !rightOK {
		return "", false, false, false
	}
	if right == "*" && left == "*" {
		return left, true, true, true
	}
	if right == "*" {
		return left, true, false, true
	}
	return left, false, false, true
}

func unquoteIdent(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	if !identQuote(token[0]) {
		if strings.ContainsAny(token, " \t`'\"\\;") {
			return "", false
		}
		return token, true
	}
	quote := token[0]
	if len(token) < 2 || token[len(token)-1] != quote {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(token)-1; i++ {
		if token[i] == quote {
			if i+1 >= len(token)-1 || token[i+1] != quote {
				return "", false
			}
			b.WriteByte(quote)
			i++
			continue
		}
		b.WriteByte(token[i])
	}
	if b.Len() == 0 && token != string([]byte{quote, quote}) {
		return "", false
	}
	return b.String(), true
}

func splitPrivileges(list string) ([]string, bool) {
	parts, ok := splitCommaList(list)
	if !ok {
		return nil, false
	}
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.ToUpper(strings.Join(strings.Fields(part), " "))
		if name == "" {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

func splitRoleSpecs(list string) ([]string, bool) {
	parts, ok := splitCommaList(list)
	if !ok {
		return nil, false
	}
	for _, part := range parts {
		if !validRoleSpec(part) {
			return nil, false
		}
	}
	return parts, true
}

func splitCommaList(list string) ([]string, bool) {
	parts, ok := splitOutsideQuotes(list, ',')
	if !ok {
		return nil, false
	}
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		parts[i] = part
	}
	return parts, true
}

func validRoleSpec(token string) bool {
	parts, ok := splitOutsideQuotes(token, '@')
	if !ok || len(parts) == 0 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !validQuotedIdent(part) {
			return false
		}
	}
	return true
}

func splitOutsideQuotes(token string, separator byte) ([]string, bool) {
	var parts []string
	var current strings.Builder
	var quote byte
	for i := 0; i < len(token); {
		if quote == 0 && token[i] == separator {
			parts = append(parts, current.String())
			current.Reset()
			i++
			continue
		}
		start := i
		next, nextQuote := advanceQuoted(token, i, quote)
		if next <= i {
			return nil, false
		}
		current.WriteString(token[start:next])
		i, quote = next, nextQuote
	}
	if quote != 0 {
		return nil, false
	}
	parts = append(parts, current.String())
	return parts, true
}

func validQuotedIdent(token string) bool {
	if len(token) < 2 || !identQuote(token[0]) || token[len(token)-1] != token[0] {
		return false
	}
	quote := token[0]
	inner := token[1 : len(token)-1]
	if inner == "" {
		return false
	}
	for i := 0; i < len(inner); i++ {
		if inner[i] == quote {
			if i+1 >= len(inner) || inner[i+1] != quote {
				return false
			}
			i++
			continue
		}
		if inner[i] < 0x20 || inner[i] == 0x7f || inner[i] == '\\' || inner[i] == ';' {
			return false
		}
	}
	return true
}
