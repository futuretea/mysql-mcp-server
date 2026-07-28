# MCP Server Template

[中文文档](README.zh.md)

Cloneable minimal MCP (Model Context Protocol) server skeleton with a dual-mode binary: **MCP server** for AI assistants and a small **CLI** for humans. Ships with example tools `echo` / `ping`.

## Initialize a derived repository

After creating a repository from this template, initialize its explicit
placeholders before building:

```bash
./scripts/init-template.sh \
  --module github.com/acme/my-mcp \
  --binary my-mcp \
  --npm-package @acme/my-mcp \
  --image ghcr.io/acme/my-mcp
```

This creates the build workflow. Add `--with-release` only when the project is
ready to publish; it also creates npm and Docker tag-release workflows. Configure
the needed registry credentials and npm registry before enabling a release.
Use `--dry-run` to inspect the planned initialization without changing files.

## Quick start

```bash
# Requires Go 1.25+ (see .tool-versions)
make build
./bin/mcp-template-binary-placeholder version

# Start MCP server (stdio, default)
./bin/mcp-template-binary-placeholder mcp

# List / call tools from the CLI (no MCP client required)
./bin/mcp-template-binary-placeholder tools list
./bin/mcp-template-binary-placeholder tools call echo --params '{"message":"hello"}'
./bin/mcp-template-binary-placeholder tools call ping --params '{}'
```

## Features

- **Subcommand CLI**: `mcp` starts the server; `tools` / `version` / `completion` are separate commands
- **Multi-transport**: stdio, Streamable HTTP, and SSE
- **Tool filtering**: `--enabled-tools`, `--disabled-tools`, `--enable-domains`, `--disable-domains`
- **Engineering defaults**: Makefile, Dockerfile, GitHub Actions CI, golangci-lint config, MIT license

## CLI commands

| Command | Purpose |
|---------|---------|
| `mcp-template-binary-placeholder mcp` | Start the MCP server (stdio or HTTP) |
| `mcp-template-binary-placeholder tools list` | List enabled tools |
| `mcp-template-binary-placeholder tools describe <name>` | Show tool schema |
| `mcp-template-binary-placeholder tools call <name>` | Invoke a tool with JSON params |
| `mcp-template-binary-placeholder version` | Print build metadata |
| `mcp-template-binary-placeholder completion <shell>` | Generate shell completion |

### Tools examples

```bash
./bin/mcp-template-binary-placeholder tools list
./bin/mcp-template-binary-placeholder tools list --json
./bin/mcp-template-binary-placeholder tools describe echo
./bin/mcp-template-binary-placeholder tools describe echo --json
./bin/mcp-template-binary-placeholder tools call echo --params '{"message":"hello"}'
echo '{"message":"hello"}' | ./bin/mcp-template-binary-placeholder tools call echo --params-file -
./bin/mcp-template-binary-placeholder tools call ping --params '{}'
```

## MCP transports

### Stdio (default)

```bash
./bin/mcp-template-binary-placeholder mcp
```

Cursor / Claude Desktop style config:

```json
{
  "mcpServers": {
    "mcp-template-binary-placeholder": {
      "command": "/absolute/path/to/bin/mcp-template-binary-placeholder",
      "args": ["mcp"]
    }
  }
}
```

### Streamable HTTP

```bash
./bin/mcp-template-binary-placeholder mcp --port 8080
curl -s http://127.0.0.1:8080/healthz
```

Client config example:

```json
{
  "mcpServers": {
    "mcp-template-binary-placeholder": {
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
./bin/mcp-template-binary-placeholder mcp --port 8080 --sse-base-url http://127.0.0.1:8080
```

### Docker

```bash
# Build
make docker

# Stdio (default ENTRYPOINT is `mcp-template-binary-placeholder mcp`)
docker run -i --rm mcp-template-image-placeholder:dev

# HTTP
docker run --rm -p 8080:8080 mcp-template-image-placeholder:dev --port 8080 --listen 0.0.0.0
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
./bin/mcp-template-binary-placeholder mcp --config config.example.yaml --port 8080
```

## Project layout

```
cmd/mcp-server/          # binary entrypoint
internal/cmd/             # cobra CLI (mcp / tools / version / completion)
pkg/core/                 # config / logging / version
pkg/server/mcp/           # MCP registration and transports
pkg/server/http/          # HTTP / SSE / healthz
pkg/toolset/              # Toolset interface and filters
pkg/toolset/example/      # sample tools (replace with your domain)
.github/workflows/        # CI
scripts/init-template.sh  # derived-project initializer
templates/                # CI and release scaffolding copied by the initializer
```

## Extend with your own tools

1. Add `pkg/toolset/<your-domain>/` implementing `toolset.Toolset`.
2. Register it in `internal/cmd/root.go` via `defaultToolsets()`.
3. Control exposure with `--enabled-tools` / `--enable-domains`.

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

Generated build CI (`.github/workflows/build.yaml`) runs lint, tests, a multi-OS
build with CLI smoke checks, and a Docker image build.

## Local reference clones

`third-party-projects/` can hold local reference checkouts. It is listed in `.gitignore` and is not part of the template deliverable.

## License

[MIT](LICENSE)
