# MySQL Read-Only MCP Server

[中文文档](README.zh.md)

An MCP (Model Context Protocol) server and CLI for one MySQL 8.0.46 target.
It provides metadata discovery and a strict read-only query tool. It does not
support writes, administration, backups, replication, or dynamic targets.

## Quick start

```bash
# Requires Go 1.25+ (see .tool-versions)
make build
./bin/mysql-mcp-server version

# Set this only in the runtime environment. Do not commit it.
export MYSQL_DSN='<runtime-dsn>'

# Start MCP server (stdio, default)
./bin/mysql-mcp-server mcp

# List / call tools from the CLI (no MCP client required)
./bin/mysql-mcp-server tools list
./bin/mysql-mcp-server tools call mysql_list_tables --params '{"database":"inventory"}'
./bin/mysql-mcp-server tools call mysql_query --params '{"sql":"SELECT 1"}'
```

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
| `mysql_query` | `sql` | Columns and safely encoded rows from one query. |

Successful calls use a JSON envelope with `schema_version: 1`. Unknown,
missing, empty, and non-string parameters are rejected before any database call.

`mysql_query` accepts one parsed read query only. It rejects multiple or
non-query statements, `INTO`, locking reads, user-variable assignments, and
blocked session/file functions. Accepted queries run in a read-only transaction
that is always rolled back.

This is not proof that stored functions, UDFs, views, or plugins have no side
effects. Use a least-privilege account; do not grant `EXECUTE`, `FILE`,
`LOCK TABLES`, or administrative privileges.

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
      "args": ["mcp"]
    }
  }
}
```

### Streamable HTTP

```bash
./bin/mysql-mcp-server mcp --port 8080
curl -s http://127.0.0.1:8080/healthz
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

```bash
# Build
make docker

# Stdio (default ENTRYPOINT is `mysql-mcp-server mcp`)
docker run -i --rm -e MYSQL_DSN ghcr.io/futuretea/mysql-mcp-server:dev

# HTTP
docker run --rm -e MYSQL_DSN -p 8080:8080 ghcr.io/futuretea/mysql-mcp-server:dev --port 8080 --listen 0.0.0.0
```

HTTP / SSE have **no auth and no TLS**. Default `--listen 127.0.0.1`. Use only on trusted networks; put a reverse proxy in front if you expose the port.

## Configuration

Priority: **flags > environment variables > config file > defaults**.

### Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_LOG_LEVEL` | Log level | `info` |
| `MCP_PORT` | HTTP port (`0` = stdio) | `0` |
| `MCP_LISTEN` | HTTP listen host | `127.0.0.1` |
| `MCP_SSE_BASE_URL` | Public SSE base URL | `""` |
| `MYSQL_DSN` | Required single MySQL runtime DSN; never logged or returned | none |

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
```

```bash
./bin/mysql-mcp-server mcp --config config.example.yaml --port 8080
```

## Project layout

```
cmd/mcp-server/          # binary entrypoint
internal/cmd/             # cobra CLI (mcp / tools / version / completion)
internal/mysql/           # connection, discovery, query boundary, result encoding
pkg/core/                 # config / logging / version
pkg/server/mcp/           # MCP registration and transports
pkg/server/http/          # HTTP / SSE / healthz
pkg/toolset/              # Toolset interface and filters
pkg/toolset/mysql/        # six MySQL tool definitions and shared parameter decoder
```

## Operational limits

This slice deliberately does not impose query timeouts, row limits, response
size limits, or concurrency budgets. Keep the server on trusted networks; HTTP
and SSE have no built-in authentication or TLS termination.

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

### Local MySQL E2E

The real MySQL E2E suite is opt-in and never runs in GitHub Actions or through
`make ci`. It starts and removes its own disposable `mysql:8.0.46` container,
then exercises the compiled CLI and MCP stdio transport:

```bash
MYSQL_MCP_INTEGRATION=1 go test ./internal/mysql -run '^TestMySQL8046Integration$' -count=1
```

## Use released artifacts

Release tags use `vX.Y.Z`. Use `X.Y.Z` for npm and `vX.Y.Z` for Docker.

```bash
npx -y @futuretea/mysql-mcp-server@<version>
docker run --rm -i ghcr.io/futuretea/mysql-mcp-server:v<version>
```

Both launch the MCP server's `mcp` command. Arguments after the package or image are forwarded to that command.

Configure an MCP client that supports stdio with npm:

```json
{
  "mcpServers": {
    "mysql-mcp-server": {
      "command": "npx",
      "args": ["-y", "@futuretea/mysql-mcp-server@<version>"]
    }
  }
}
```

Supported release platforms are recorded in `.github/ci/release-platforms.json`.

## License

[MIT](LICENSE)
