# MCP Server Template

[English](README.md)

可 clone 的最小 MCP（Model Context Protocol）server 骨架。同一二进制同时提供给 AI 助手用的 **MCP server**，以及给人用的小型 **CLI**。内置示例工具 `echo` / `ping`。

## 初始化派生仓库

从此模板创建仓库后，先替换明确的占位符，再开始构建：

```bash
./scripts/init-template.sh \
  --module github.com/acme/my-mcp \
  --binary my-mcp \
  --npm-package @acme/my-mcp \
  --image ghcr.io/acme/my-mcp
```

该命令会生成构建工作流。只有项目已准备发布时才加 `--with-release`；它会额外生成 npm 和 Docker 的 tag 发布工作流。启用发布前，请先配置所需的镜像仓库凭证和 npm registry。使用 `--dry-run` 可只查看计划，不修改文件。

## 快速开始

```bash
# 需要 Go 1.25+（见 .tool-versions）
make build
./bin/mcp-template-binary-placeholder version

# 启动 MCP server（默认 stdio）
./bin/mcp-template-binary-placeholder mcp

# 不经过 MCP 客户端，直接用 CLI 列工具 / 调工具
./bin/mcp-template-binary-placeholder tools list
./bin/mcp-template-binary-placeholder tools call echo --params '{"message":"hello"}'
./bin/mcp-template-binary-placeholder tools call ping --params '{}'
```

## 功能

- **子命令 CLI**：`mcp` 才启动 server；`tools` / `version` / `completion` 独立
- **多传输**：stdio、Streamable HTTP、SSE
- **工具过滤**：`--enabled-tools`、`--disabled-tools`、`--enable-domains`、`--disable-domains`
- **工程化默认项**：Makefile、Dockerfile、GitHub Actions CI、golangci-lint、MIT License

## CLI 命令

| 命令 | 作用 |
|------|------|
| `mcp-template-binary-placeholder mcp` | 启动 MCP server（stdio 或 HTTP） |
| `mcp-template-binary-placeholder tools list` | 列出已启用工具 |
| `mcp-template-binary-placeholder tools describe <name>` | 查看工具 schema |
| `mcp-template-binary-placeholder tools call <name>` | 用 JSON 参数调用工具 |
| `mcp-template-binary-placeholder version` | 打印构建信息 |
| `mcp-template-binary-placeholder completion <shell>` | 生成 shell 补全脚本 |

### tools 示例

```bash
./bin/mcp-template-binary-placeholder tools list
./bin/mcp-template-binary-placeholder tools list --json
./bin/mcp-template-binary-placeholder tools describe echo
./bin/mcp-template-binary-placeholder tools describe echo --json
./bin/mcp-template-binary-placeholder tools call echo --params '{"message":"hello"}'
echo '{"message":"hello"}' | ./bin/mcp-template-binary-placeholder tools call echo --params-file -
./bin/mcp-template-binary-placeholder tools call ping --params '{}'
```

## MCP 传输模式

### Stdio（默认）

```bash
./bin/mcp-template-binary-placeholder mcp
```

Cursor / Claude Desktop 配置示例：

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

客户端配置示例：

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

与 HTTP 模式同一进程。端点：

| 路径 | 用途 |
|------|------|
| `/healthz` | 健康检查（`GET`/`HEAD`） |
| `/mcp` | Streamable HTTP |
| `/sse` | SSE 连接 |
| `/message` | SSE message endpoint |

```bash
./bin/mcp-template-binary-placeholder mcp --port 8080 --sse-base-url http://127.0.0.1:8080
```

### Docker

```bash
# 构建
make docker

# Stdio（默认 ENTRYPOINT 为 mcp-template-binary-placeholder mcp）
docker run -i --rm mcp-template-image-placeholder:dev

# HTTP
docker run --rm -p 8080:8080 mcp-template-image-placeholder:dev --port 8080 --listen 0.0.0.0
```

HTTP / SSE **无鉴权、无 TLS**。默认 `--listen 127.0.0.1`。仅在受信网络使用；对外暴露时请自行加反向代理与认证。

## 配置

优先级：**flags > 环境变量 > 配置文件 > 默认值**。

### 环境变量

| 变量 | 说明 | 默认 |
|------|------|------|
| `MCP_LOG_LEVEL` | 日志级别 | `info` |
| `MCP_PORT` | HTTP 端口（`0` = stdio） | `0` |
| `MCP_LISTEN` | HTTP 监听地址 | `127.0.0.1` |
| `MCP_SSE_BASE_URL` | 对外 SSE base URL | `""` |

### 配置文件

见 `config.example.yaml`：

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

## 目录结构

```
cmd/mcp-server/          # 入口
internal/cmd/            # cobra CLI（mcp / tools / version / completion）
pkg/core/                # config / logging / version
pkg/server/mcp/          # MCP 注册与 transport
pkg/server/http/         # HTTP / SSE / healthz
pkg/toolset/             # Toolset 接口与过滤
pkg/toolset/example/     # 示例 tools（替换为你的业务）
.github/workflows/       # CI
scripts/init-template.sh # 派生仓库初始化脚本
templates/               # 由初始化脚本复制的 CI 与发布骨架
```

## 扩展自己的 tools

1. 在 `pkg/toolset/<your-domain>/` 实现 `toolset.Toolset`。
2. 在 `internal/cmd/root.go` 的 `defaultToolsets()` 中挂载。
3. 用 `--enabled-tools` / `--enable-domains` 控制暴露面。

## 开发

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

生成的构建 CI（`.github/workflows/build.yaml`）会跑 lint、测试、多 OS 构建与 CLI smoke（`version` / `tools list`），以及 Docker 镜像构建。

## 本地对照样例

`third-party-projects/` 可放置本地对照代码，已在 `.gitignore` 中，不属于模板交付物。

## License

[MIT](LICENSE)
