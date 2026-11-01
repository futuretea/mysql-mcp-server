package mysql

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
)

const startupRoleOptOutMessage = "database service role is not permitted; allow_unsafe_database_role is set, so the startup role check is skipped"

const showCurrentGrantsSQL = "SHOW GRANTS FOR CURRENT_USER()"

const maxRoleExpansions = 8

// unsafePrivilegeNames are global administrative privileges rejected at startup.
// Static names are the MySQL privilege spellings from SHOW GRANTS. Dynamic
// names are the MySQL 8 privileges that replace SUPER or administer accounts,
// server files, replication, backup, and server control.
var unsafePrivilegeNames = []string{
	"SUPER",
	"FILE",
	"PROCESS",
	"SHUTDOWN",
	"RELOAD",
	"CREATE USER",
	"CREATE ROLE",
	"DROP ROLE",
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
	"FIREWALL_EXEMPT",
	"FLUSH_OPTIMIZER_COSTS",
	"FLUSH_STATUS",
	"FLUSH_TABLES",
	"FLUSH_USER_RESOURCES",
	"GROUP_REPLICATION_ADMIN",
	"GROUP_REPLICATION_STREAM",
	"INNODB_REDO_LOG_ARCHIVE",
	"INNODB_REDO_LOG_ENABLE",
	"PASSWORDLESS_USER_ADMIN",
	"PERSIST_RO_VARIABLES_ADMIN",
	"REPLICATION_APPLIER",
	"REPLICATION_SLAVE_ADMIN",
	"RESOURCE_GROUP_ADMIN",
	"ROLE_ADMIN",
	"SENSITIVE_VARIABLES_OBSERVER",
	"SERVICE_CONNECTION_ADMIN",
	"SESSION_VARIABLES_ADMIN",
	"SET_ANY_DEFINER",
	"SET_USER_ID",
	"SYSTEM_USER",
	"SYSTEM_VARIABLES_ADMIN",
	"TABLE_ENCRYPTION_ADMIN",
	"TELEMETRY_LOG_ADMIN",
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
	upper := strings.ToUpper(value)
	key := strings.ToUpper(keyword)
	inQuote := false
	for i := 0; i < len(value); i++ {
		if value[i] == '`' {
			if inQuote && i+1 < len(value) && value[i+1] == '`' {
				i++
				continue
			}
			inQuote = !inQuote
			continue
		}
		if inQuote || i+len(key) > len(value) || upper[i:i+len(key)] != key {
			continue
		}
		return i
	}
	return -1
}

func privilegesUnsafe(names []string, scope string) bool {
	global := isGlobalScope(scope)
	for _, name := range names {
		switch name {
		case "ALL", "ALL PRIVILEGES":
			if global {
				return true
			}
		default:
			if _, ok := unsafePrivileges[name]; ok {
				return true
			}
		}
	}
	return false
}

func isGlobalScope(scope string) bool {
	var normalized strings.Builder
	for _, r := range scope {
		if r == '`' || r == ' ' || r == '\t' {
			continue
		}
		normalized.WriteRune(r)
	}
	return strings.EqualFold(normalized.String(), "*.*")
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
	var parts []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(list); i++ {
		ch := list[i]
		if ch == '`' {
			if inQuote && i+1 < len(list) && list[i+1] == '`' {
				current.WriteString("``")
				i++
				continue
			}
			inQuote = !inQuote
			current.WriteByte(ch)
			continue
		}
		if ch == ',' && !inQuote {
			part := strings.TrimSpace(current.String())
			if part == "" {
				return nil, false
			}
			parts = append(parts, part)
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	if inQuote {
		return nil, false
	}
	part := strings.TrimSpace(current.String())
	if part == "" {
		return nil, false
	}
	parts = append(parts, part)
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
	inQuote := false
	for i := 0; i < len(token); i++ {
		ch := token[i]
		if ch == '`' {
			if inQuote && i+1 < len(token) && token[i+1] == '`' {
				current.WriteString("``")
				i++
				continue
			}
			inQuote = !inQuote
			current.WriteByte(ch)
			continue
		}
		if ch == separator && !inQuote {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	if inQuote {
		return nil, false
	}
	parts = append(parts, current.String())
	return parts, true
}

func validQuotedIdent(token string) bool {
	if len(token) < 2 || token[0] != '`' || token[len(token)-1] != '`' {
		return false
	}
	inner := token[1 : len(token)-1]
	if inner == "" {
		return false
	}
	for i := 0; i < len(inner); i++ {
		if inner[i] == '`' {
			if i+1 >= len(inner) || inner[i+1] != '`' {
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
