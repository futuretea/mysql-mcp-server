# MySQL Read-Only MCP Server

[中文文档](README.zh.md)

An MCP (Model Context Protocol) server and CLI for one MySQL connection.
It provides metadata discovery and a strict read-only query tool. It does not
support writes, administration, backups, replication, or a second target in
the same process.

The integration tests use the image `mysql:8.0.46`. Calling `mysql_get_server_info` runs `SELECT VERSION(), @@version_comment, DATABASE()`. Startup does not read `VERSION()`, and it does not reject other server versions.

## Quick start

```bash
# Use the Go version in .tool-versions and go.mod (1.25.10).
make build
./bin/mysql-mcp-server version

# Set these only in the runtime environment. Do not commit the password.
export MYSQL_MCP_HOST='127.0.0.1'
export MYSQL_MCP_DB_PORT='3306'
export MYSQL_MCP_USERNAME='readonly'
export MYSQL_MCP_PASSWORD='<runtime-secret>'
export MYSQL_MCP_DATABASE='inventory'

# Start MCP server (stdio, default)
./bin/mysql-mcp-server mcp

# List tools, or call a tool, without an MCP client
./bin/mysql-mcp-server tools list
./bin/mysql-mcp-server tools call mysql_list_tables --params '{"database":"inventory"}'
./bin/mysql-mcp-server tools call mysql_query --params '{"sql":"SELECT 1"}'
```

`make build` writes `./bin/mysql-mcp-server`. `version` prints one line, `mysql-mcp-server version=<version> commit=<commit> date=<date>`. A build without release ldflags prints `version=dev`.

`tools list` and `tools describe` do not open a MySQL connection. `mcp` and `tools call` do. With the environment variables above and a reachable MySQL account, `tools call mysql_query` returns a JSON envelope. If MySQL is not listening, the command exits 1 and prints `[mysql_connection_failed] connection failed`.

## Features

- Six MySQL discovery and query tools are available through both MCP and CLI.
- JSON parameter validation occurs at the shared tool-handler boundary.
- A Vitess AST query boundary and `database/sql` read-only transaction protect accepted queries.
- Stdio, Streamable HTTP, and SSE transports are supported.

## MySQL tools

| Tool | Parameters | Result |
|---|---|---|
| `mysql_get_server_info` | none | Version, version comment, and current database. |
| `mysql_list_databases` | none | Databases visible to the runtime account. |
| `mysql_list_tables` | `database` | Base tables and views in a database. |
| `mysql_describe_table` | `database`, `table` | Column metadata. |
| `mysql_list_indexes` | `database`, `table` | Indexes and key parts. |
| `mysql_query` | `sql` | Columns and encoded rows from one query. |

Successful calls use a JSON envelope with `schema_version: 1`. A non-null query cell is `{"encoding","value"}`. `encoding` is `integer`, `float`, `utf8`, or `base64`. SQL `NULL` is JSON `null`. A driver value outside that set fails with `[mysql_result_encoding_failed] result encoding failed`. Before any database call, an argument set that is not a JSON object, or a value that is unknown, missing, empty, or not a string, returns `[mysql_invalid_params] invalid parameters`.

`mysql_query` accepts one parsed read query only. It rejects multiple or
non-query statements, `INTO`, locking reads, user-variable assignments, and
blocked session/file functions. Accepted queries run in a read-only transaction
that is always rolled back.

This is not proof that stored functions, UDFs, views, or plugins have no side
effects. Use a least-privilege account. Startup rejects `EXECUTE`, schema-wide `ALL`, `FILE`,
`SHOW_ROUTINE`, grants on the `mysql` schema, global `SELECT`, and other
administrative privileges unless `allow_unsafe_database_role` is set. See
[Service account](#service-account).

## CLI commands

| Command | Purpose |
|---------|---------|
| `mysql-mcp-server mcp` | Start the MCP server (stdio or HTTP) |
| `mysql-mcp-server tools list` | List enabled tools |
| `mysql-mcp-server tools describe <name>` | Show tool schema |
| `mysql-mcp-server tools call <name>` | Invoke a tool with JSON params |
| `mysql-mcp-server version` | Print build metadata |
| `mysql-mcp-server completion <shell>` | Generate shell completion |

### Tools examples

```bash
./bin/mysql-mcp-server tools list
./bin/mysql-mcp-server tools list --json
./bin/mysql-mcp-server tools describe mysql_query
./bin/mysql-mcp-server tools describe mysql_query --json
./bin/mysql-mcp-server tools call mysql_list_tables --params '{"database":"inventory"}'
echo '{"sql":"SELECT 1"}' | ./bin/mysql-mcp-server tools call mysql_query --params-file -
```

## MCP transports

### Stdio (default)

```bash
./bin/mysql-mcp-server mcp
```

Cursor / Claude Desktop style config:

```json
{
  "mcpServers": {
    "mysql-mcp-server": {
      "command": "/absolute/path/to/bin/mysql-mcp-server",
      "args": ["mcp"],
      "env": {
        "MYSQL_MCP_HOST": "127.0.0.1",
        "MYSQL_MCP_DB_PORT": "3306",
        "MYSQL_MCP_USERNAME": "readonly",
        "MYSQL_MCP_PASSWORD": "<runtime-secret>",
        "MYSQL_MCP_DATABASE": "inventory"
      }
    }
  }
}
```

### Streamable HTTP

```bash
./bin/mysql-mcp-server mcp --port 8080
curl -s http://127.0.0.1:8080/healthz
# healthy
```

Client config example:

```json
{
  "mcpServers": {
    "mysql-mcp-server": {
      "url": "http://127.0.0.1:8080/mcp"
    }
  }
}
```

### SSE

Same process as HTTP mode. Endpoints:

| Path | Purpose |
|------|---------|
| `/healthz` | Health check (`GET`/`HEAD`) |
| `/mcp` | Streamable HTTP |
| `/sse` | SSE connection |
| `/message` | SSE message endpoint |

```bash
./bin/mysql-mcp-server mcp --port 8080 --sse-base-url http://127.0.0.1:8080
```

### Docker

`make docker` builds a local image. It tags that image `ghcr.io/futuretea/mysql-mcp-server:dev` and `ghcr.io/futuretea/mysql-mcp-server:<git describe>`. The `:dev` tag exists only on the machine that built it. GHCR does not publish `:dev`.

```bash
make docker

# Stdio. The image ENTRYPOINT is `mysql-mcp-server mcp`.
docker run -i --rm \
  -e MYSQL_MCP_HOST -e MYSQL_MCP_DB_PORT -e MYSQL_MCP_USERNAME \
  -e MYSQL_MCP_PASSWORD -e MYSQL_MCP_DATABASE \
  ghcr.io/futuretea/mysql-mcp-server:dev

# HTTP. Publish the host port only to loopback.
docker run --rm \
  -e MYSQL_MCP_HOST -e MYSQL_MCP_DB_PORT -e MYSQL_MCP_USERNAME \
  -e MYSQL_MCP_PASSWORD -e MYSQL_MCP_DATABASE \
  -p 127.0.0.1:8080:8080 \
  ghcr.io/futuretea/mysql-mcp-server:dev --port 8080 --listen 0.0.0.0
```

`curl -s http://127.0.0.1:8080/healthz` prints `healthy`.

The published image tags are under [Use released artifacts](#use-released-artifacts).

HTTP and SSE have no authentication and no TLS. The default `--listen` value is `127.0.0.1`. Put a reverse proxy in front of any listener that is not loopback.

## Configuration

Connection settings and server settings load differently.

### MySQL connection

The server reads the connection only from these environment variables. A flag, a YAML key, and `MYSQL_DSN` do not set the host, port, user, password, or database.

| Variable | Description | Default |
|----------|-------------|---------|
| `MYSQL_MCP_HOST` | MySQL TCP host | `127.0.0.1` |
| `MYSQL_MCP_DB_PORT` | MySQL TCP port. This is not the HTTP port. | `3306` |
| `MYSQL_MCP_USERNAME` | MySQL user | none |
| `MYSQL_MCP_PASSWORD` | MySQL password. The server does not log it or copy it into errors. | empty |
| `MYSQL_MCP_DATABASE` | Default database | none |

`MYSQL_MCP_USERNAME` must be set before a tool can query. If it is empty, `mcp` still starts and writes this line to stderr:

```text
mysql runtime configuration unavailable: MYSQL_MCP_USERNAME is required
```

A tool call then returns `[mysql_configuration_unavailable] configuration unavailable`. `tools call` exits 1. `tools list` and `tools describe` do not read these variables.

A refused or unreachable address returns `[mysql_connection_failed] connection failed`. The privilege check does not run until the connection succeeds.

### Server settings

`--port`, `--listen`, `--sse-base-url`, and `--log-level` exist only on `mysql-mcp-server mcp`. `tools` rejects those flags. On `mcp`, a flag you pass wins, then the environment variable, then the YAML file, then the default. Names such as `MCP_PORT`, without the `MYSQL_MCP_` prefix, are ignored.

| Setting | `mcp` flag | Environment variable | YAML key | Default |
|---|---|---|---|---|
| HTTP port. `0` keeps stdio. | `--port` | `MYSQL_MCP_PORT` | `port` | `0` |
| HTTP listen host | `--listen` | `MYSQL_MCP_LISTEN` | `listen` | `127.0.0.1` |
| Public SSE base URL | `--sse-base-url` | `MYSQL_MCP_SSE_BASE_URL` | `sse_base_url` | empty |
| Log level | `--log-level` | `MYSQL_MCP_LOG_LEVEL` | `log_level` | `info` |

`tools` reads the log level from `MYSQL_MCP_LOG_LEVEL` or YAML `log_level`. It has no `--log-level` flag. Log levels are `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic`, and `disabled`. In stdio mode and during `tools call`, the server writes only `error` and more severe levels, even when you set a lower level. The unsafe-role warning is not shown there. HTTP mode uses the level you set. At the default `info` level, HTTP mode shows that warning.

`--config`, `--enabled-tools`, `--disabled-tools`, `--enable-domains`, and `--disable-domains` are persistent flags. They work on `mcp` and on `tools`. The same order applies: flag, then environment variable, then YAML, then default.

| Setting | Flag | Environment variable | YAML key | Default |
|---|---|---|---|---|
| Tools to enable | `--enabled-tools` | `MYSQL_MCP_ENABLED_TOOLS` | `enabled_tools` | all tools |
| Tools to disable | `--disabled-tools` | `MYSQL_MCP_DISABLED_TOOLS` | `disabled_tools` | none |
| Domains to enable | `--enable-domains` | `MYSQL_MCP_ENABLED_DOMAINS` | `enabled_domains` | all domains |
| Domains to disable | `--disable-domains` | `MYSQL_MCP_DISABLED_DOMAINS` | `disabled_domains` | none |
| Skip only the unsafe-privilege rejection | none | `MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE` | `allow_unsafe_database_role` | `false` |

Pass tool and domain lists as comma-separated names. The tool domain is `mysql`.

```bash
./bin/mysql-mcp-server tools list --enabled-tools mysql_query,mysql_list_databases
MYSQL_MCP_ENABLED_TOOLS=mysql_query ./bin/mysql-mcp-server tools list
```

The first command prints `mysql_query` and `mysql_list_databases`. The second prints `mysql_query`.

`MYSQL_MCP_MASKING_JSON` is not one of the rows above. When that variable is present, it replaces the whole YAML `masking` object. See [Output masking](#output-masking).

### Service account

After the process connects, and before any tool runs SQL, startup reads `SHOW GRANTS` for the current account. Plain `SHOW GRANTS` includes mandatory roles; `SHOW GRANTS FOR CURRENT_USER()` does not. Privileges inherited through those roles and through explicitly granted roles are expanded before any tool runs. The process exits with `[mysql_unsafe_service_role]` when the account has any of:

- `SUPER`, `FILE`, `PROCESS`, `SHUTDOWN`, `RELOAD`, `CREATE USER`, `CREATE TABLESPACE`, or `PROXY`
- `CREATE ROLE`, `DROP ROLE`, or `EXECUTE`
- `REPLICATION SLAVE` or `REPLICATION CLIENT`
- `SHOW_ROUTINE` or `RESOURCE_GROUP_USER`
- global `SELECT`, `ALL`, or `ALL PRIVILEGES` (`ON *.*`)
- schema-wide `ALL` or `ALL PRIVILEGES` (`ON db.*`). MySQL stores `EXECUTE` on that database privilege row and `SHOW GRANTS` keeps the shorthand, so the word `EXECUTE` never appears. `ALL` on one table (`ON db.table`) does not include `EXECUTE`
- any privilege on the `mysql` schema, including `SELECT` on `mysql.user`
- MySQL 8 dynamic privileges that replace `SUPER` or administer the server, including `SYSTEM_VARIABLES_ADMIN`, `SESSION_VARIABLES_ADMIN`, `CONNECTION_ADMIN`, `TP_CONNECTION_ADMIN`, `SYSTEM_USER`, `ROLE_ADMIN`, `BINLOG_ADMIN`, `REPLICATION_SLAVE_ADMIN`, `SET_USER_ID`, `ALLOW_NONEXISTENT_DEFINER`, `BACKUP_ADMIN`, `CLONE_ADMIN`, `FLUSH_PRIVILEGES`, `FLUSH_TABLES`, `FIREWALL_ADMIN`, `OPTIMIZE_LOCAL_TABLE`, and `VERSION_TOKEN_ADMIN`

`USAGE` plus `SELECT` on an application schema starts with the default configuration. No opt-out is required. `SELECT` on `*.*` does not start: that grant includes the `mysql` grant tables. An expanded database privilege list that does not include `EXECUTE` also starts. A routine call then fails in MySQL rather than at startup.

A view created by a DBA with `SQL SECURITY DEFINER` is published schema. An account that can `SELECT` the view can read its output, including a `mysql.user` count or a function result, without holding `EXECUTE`. Startup does not classify that body. For this account `VIEW_DEFINITION` is empty and `SHOW CREATE VIEW` is denied, so the check cannot tell a normal default-definer application view from one that publishes catalog data. Do not publish a definer view that exposes data the service account should not see. That boundary is not a substitute for rejecting `EXECUTE` or schema-wide `ALL`.

Startup allows 5 seconds for the TCP connection and the grant probe. A server that does not answer in that time stops startup. `allow_unsafe_database_role` does not extend the deadline.

`allow_unsafe_database_role` defaults to `false`. Set it to `true` in the config file, or export `MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE=true`, only when you cannot provision a least-privilege account. The server logs a warning and skips this privilege rejection. A refused connection, a failed grant probe, a startup timeout, and every other startup check still stop startup. Queries still run in a read-only transaction, but that transaction does not constrain `FILE`, `SHUTDOWN`, `SUPER`, replication privileges, `PROXY`, `EXECUTE`, or the dynamic privileges above. Enable the option only for a MySQL instance you control.

Stdio MCP and `tools call` keep logs at error level so protocol output stays intact. The opt-out warning is emitted at warn level and is visible in HTTP mode at the default `info` level.

### Config file

See `config.example.yaml`:

```yaml
port: 0
listen: 127.0.0.1
sse_base_url: ""
log_level: info
enabled_tools: []
disabled_tools: []
enabled_domains: []
disabled_domains: []
allow_unsafe_database_role: false
```

```bash
./bin/mysql-mcp-server mcp --config config.example.yaml --port 8080
```

## Project layout

```
cmd/mcp-server/          # binary entrypoint
internal/cmd/             # cobra CLI (mcp / tools / version / completion)
internal/mysql/           # connection, discovery, query boundary, result encoding
pkg/core/                 # config, logging, version
pkg/core/masking/         # output masking policy
pkg/server/mcp/           # MCP registration and transports
pkg/server/http/          # HTTP / SSE / healthz
pkg/toolset/              # Toolset interface and filters
pkg/toolset/mysql/        # six MySQL tool definitions and shared parameter decoder
```

## Output masking

Masking is off when the `masking` object is absent, when `enabled` is omitted, or when `enabled` is false. Tool results then keep their current JSON. Set `enabled` to true and at least one rule to mask configured data. `MYSQL_MCP_MASKING_JSON` replaces the whole YAML object when the variable is present. An empty value is rejected. Unset leaves the YAML object in place.

```yaml
masking:
  enabled: true
  placeholder: "[masked]"
  names: ["synthetic_field"]
  name_patterns: ["token_.+"]
  values: ["synthetic-token-value"]
  value_patterns: ["sk_live_[0-9]+"]
```

```bash
export MYSQL_MCP_MASKING_JSON='{"enabled":true,"names":["synthetic_field"],"values":["synthetic-token-value"]}'
```

Name rules match a whole column, table, index, or database name, and a decoded JSON object key. The pattern is parsed before it is anchored, so a fragment such as `a)|(b` is rejected instead of matching a prefix or a suffix. A value pattern uses RE2 leftmost-first matching and does not look for a longer alternative. A matched string becomes the placeholder and keeps a MySQL `{"encoding","value"}` cell when the cell was text or base64. A matched number or other non-string becomes JSON `null`. An existing SQL `NULL` stays `null`. A JSON `null` in a masked result can therefore be either a database NULL or a redacted value, and the JSON type can change.

Value rules are substring matches on the original text, including one-character literals. They run once and do not search the placeholder. A pattern is rejected when any alternative can match without consuming a character, such as a word-boundary branch. JSON objects and arrays stored in utf8 cells are read as JSON, including escaped keys. A value rule that hits a JSON number or bool, or a MySQL integer or float cell, replaces that whole value with null and does not splice the digits. An unmatched number keeps its original text. Standard base64 cells are decoded, masked, and encoded again, including a value pattern that matches an ASCII secret next to invalid UTF-8. Ordinary prose that looks like base64 is not decoded. This is not a detector for every private value, compressed payload, or encoding.

A bad masking document fails when configuration is loaded, before a tool runs. Each of `names`, `name_patterns`, `values`, and `value_patterns` accepts at most 32 entries. A name is at most 128 bytes. A value literal is at most 256 bytes. A pattern is at most 128 bytes. The placeholder must be 1 to 64 bytes. `enabled: true` with no rules fails the same way. The command prints `load config: invalid masking config` and does not echo the document.

While masking is on, one tool result uses one shared budget. The budget counts nodes, and it counts bytes passed to `Budget.add`. A text replacement charges the output size from `outputSize`. Re-encoded JSON charges the encoded length. A number or bool check charges the original text length. The budget allows 10,000 nodes and 1 MiB of those charged bytes. Separately, one input string longer than 1 MiB is rejected. JSON stored inside a string is masked for 2 rounds. A further structural JSON object or array fails the result. JSON deeper than 32 levels fails the result. More than 4,096 matches in one value fails the result. The call returns `[mysql_masking_failed] result was not returned` and no partial payload.

`schema_version` and the tool name are server constants and are not masked. Fixed public errors are not rewritten, so a short literal that also occurs in an error sentence or in the placeholder can still appear there. The guarantee is that the original configured data is removed from the covered result text.

Duplicate JSON keys, two masked names that collide, or an unsupported cell also return `[mysql_masking_failed] result was not returned`. The database command is unchanged.

HTTP debug logs record a route class (`mcp`, `sse`, `message`, or `other`) instead of the raw path or query. The masker does not write the source value to a log.

## Operational limits

Query execution has no timeout, row limit, response-size limit, or concurrency budget. The masking limits above apply only while masking is on. HTTP and SSE have no authentication and no TLS. Keep a non-loopback listener on a trusted network.

### Errors the server returns

These strings are fixed. They do not include SQL text, the password, or the masking document.

| Text | When |
|---|---|
| `[mysql_configuration_unavailable] configuration unavailable` | The connection environment is missing or the service was not created. |
| `[mysql_invalid_params] invalid parameters` | The arguments are not a JSON object, or a value is unknown, missing, empty, or not a string. No database call is made. |
| `[mysql_connection_failed] connection failed` | The driver error is a deadline, `driver.ErrBadConn`, `sql.ErrConnDone`, or a `net.Error`. |
| `[mysql_unsafe_service_role] database service role is not permitted` | The account has a rejected privilege and the opt-out is false. |
| `[mysql_query_rejected] query rejected` | `mysql_query` refuses the SQL text. |
| `[mysql_database_error] database error` | MySQL returns another error, including a `SHOW GRANTS` failure that is not one of the connection cases above. Driver text is not copied. |
| `[mysql_result_encoding_failed] result encoding failed` | A cell type is outside the documented encodings. |
| `[mysql_masking_failed] result was not returned` | Masking cannot finish inside its bounds. |
| `load config: invalid masking config` | Configuration load rejects the masking document. The document is not echoed. |

## Development

```bash
make tidy
make format
make lint
make test
make coverage
make ci
make build
make docker
```

### Tests that use MySQL

`make ci` runs lint, `go test ./...`, and the build. The MySQL integration tests skip unless `MYSQL_MCP_INTEGRATION=1`, so `make ci` does not start MySQL.

The GitHub Actions job `MySQL integration` sets `MYSQL_MCP_INTEGRATION=1` and runs `TestMySQL8046Integration` and `TestStartupPrivilegesIntegration`. Each test starts and removes its own `mysql:8.0.46` container. `TestMySQL8046Integration` checks query behavior. It expects `mysql_get_server_info` to return a version string that starts with `8.0.46`. Startup does not read or enforce the server version.

Run the same tests locally when Docker can pull that image:

```bash
MYSQL_MCP_INTEGRATION=1 go test ./internal/mysql -count=1 -timeout 12m -run 'TestMySQL8046Integration|TestStartupPrivilegesIntegration'
```

## Use released artifacts

The latest published release is `0.2.0`. Release tags use `vX.Y.Z`. npm uses `X.Y.Z`. Docker uses `vX.Y.Z`. GitHub Release pages for these tags do not attach binaries. Install with npm or Docker.

```bash
npx -y @futuretea/mysql-mcp-server@0.2.0
docker run --rm -i \
  -e MYSQL_MCP_HOST -e MYSQL_MCP_DB_PORT -e MYSQL_MCP_USERNAME \
  -e MYSQL_MCP_PASSWORD -e MYSQL_MCP_DATABASE \
  ghcr.io/futuretea/mysql-mcp-server:v0.2.0
```

`ghcr.io/futuretea/mysql-mcp-server:latest` is the same release. Older published versions remain available: npm `0.0.2` and `0.1.0`, and Docker `v0.0.2` and `v0.1.0`. `0.2.0` is the first release that masks tool output. There is no npm `0.0.1` and no GHCR `v0.0.1` or `:dev`.

The npm package `@futuretea/mysql-mcp-server@0.2.0` still contains the README from that release. The README in this repository is the corrected text. Replacing the README inside the published tarball requires a new release, and this change does not publish one.

Both commands start `mysql-mcp-server mcp`. Arguments after the package or image are flags for `mcp`, such as `--port 8080`. `npx` does not run `tools` or `version`. Use `make build` for those subcommands.

npm publishes `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, and `windows/amd64`. The Docker image index for `v0.2.0` contains `linux/amd64` and `linux/arm64`. The same inventory is in `.github/ci/release-platforms.json`.

Configure an MCP client that supports stdio with npm:

```json
{
  "mcpServers": {
    "mysql-mcp-server": {
      "command": "npx",
      "args": ["-y", "@futuretea/mysql-mcp-server@0.2.0"],
      "env": {
        "MYSQL_MCP_HOST": "127.0.0.1",
        "MYSQL_MCP_DB_PORT": "3306",
        "MYSQL_MCP_USERNAME": "readonly",
        "MYSQL_MCP_PASSWORD": "<runtime-secret>",
        "MYSQL_MCP_DATABASE": "inventory"
      }
    }
  }
}
```

## License

[MIT](LICENSE)
