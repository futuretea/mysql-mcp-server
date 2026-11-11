# MySQL 只读 MCP Server

[English](README.md)

连接单个 MySQL 8.0.46 目标的 MCP（Model Context Protocol）服务端与 CLI。它提供元数据发现和严格只读查询；不支持写入、管理、备份、复制或运行时切换目标。

## 快速开始

```bash
# 需要 Go 1.25+（见 .tool-versions）
make build
./bin/mysql-mcp-server version

# 只在运行环境设置，密码禁止提交。
export MYSQL_MCP_HOST='127.0.0.1'
export MYSQL_MCP_DB_PORT='3306'
export MYSQL_MCP_USERNAME='readonly'
export MYSQL_MCP_PASSWORD='<runtime-secret>'
export MYSQL_MCP_DATABASE='inventory'

# 启动 MCP server（默认 stdio）
./bin/mysql-mcp-server mcp

# 不经过 MCP 客户端，直接用 CLI 列工具 / 调工具
./bin/mysql-mcp-server tools list
./bin/mysql-mcp-server tools call mysql_list_tables --params '{"database":"inventory"}'
./bin/mysql-mcp-server tools call mysql_query --params '{"sql":"SELECT 1"}'
```

## 功能

- 六个 MySQL 发现与查询工具，同时通过 MCP 和 CLI 提供。
- 在共享 tool handler 边界严格校验 JSON 参数。
- 使用 Vitess AST 查询边界和 `database/sql` 只读事务。
- 支持 stdio、Streamable HTTP 和 SSE。

## MySQL 工具

| 工具 | 参数 | 返回 |
|---|---|---|
| `mysql_get_server_info` | 无 | 版本、版本说明和当前 database。 |
| `mysql_list_databases` | 无 | 运行账号可见的 databases。 |
| `mysql_list_tables` | `database` | 指定 database 的基础表和视图。 |
| `mysql_describe_table` | `database`、`table` | 列元数据。 |
| `mysql_list_indexes` | `database`、`table` | 索引与 key parts。 |
| `mysql_query` | `sql` | 单条查询的列和安全编码后的 rows。 |

成功调用使用带 `schema_version: 1` 的同一 JSON envelope。未知、缺失、空字符串和非字符串参数会在访问数据库之前拒绝。

`mysql_query` 只接受一条解析后的读查询。它拒绝多语句、非查询语句、`INTO`、锁定读、用户变量赋值和已阻断的会话/文件函数。通过后查询在只读事务中执行，并始终回滚。

这不能证明存储函数、UDF、视图或插件没有副作用。请使用最小权限账号。除非设置 `allow_unsafe_database_role`，启动时会拒绝 `EXECUTE`、`FILE`、`SHOW_ROUTINE`、`mysql` 系统库上的授权、全局 `SELECT` 以及其它管理权限。见[服务账号](#服务账号)。

## CLI 命令

| 命令 | 作用 |
|------|------|
| `mysql-mcp-server mcp` | 启动 MCP server（stdio 或 HTTP） |
| `mysql-mcp-server tools list` | 列出已启用工具 |
| `mysql-mcp-server tools describe <name>` | 查看工具 schema |
| `mysql-mcp-server tools call <name>` | 用 JSON 参数调用工具 |
| `mysql-mcp-server version` | 打印构建信息 |
| `mysql-mcp-server completion <shell>` | 生成 shell 补全脚本 |

### tools 示例

```bash
./bin/mysql-mcp-server tools list
./bin/mysql-mcp-server tools list --json
./bin/mysql-mcp-server tools describe mysql_query
./bin/mysql-mcp-server tools describe mysql_query --json
./bin/mysql-mcp-server tools call mysql_list_tables --params '{"database":"inventory"}'
echo '{"sql":"SELECT 1"}' | ./bin/mysql-mcp-server tools call mysql_query --params-file -
```

## MCP 传输模式

### Stdio（默认）

```bash
./bin/mysql-mcp-server mcp
```

Cursor / Claude Desktop 配置示例：

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
```

客户端配置示例：

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

与 HTTP 模式同一进程。端点：

| 路径 | 用途 |
|------|------|
| `/healthz` | 健康检查（`GET`/`HEAD`） |
| `/mcp` | Streamable HTTP |
| `/sse` | SSE 连接 |
| `/message` | SSE message endpoint |

```bash
./bin/mysql-mcp-server mcp --port 8080 --sse-base-url http://127.0.0.1:8080
```

### Docker

```bash
# 构建
make docker

# Stdio（默认 ENTRYPOINT 为 mysql-mcp-server mcp）
docker run -i --rm \
  -e MYSQL_MCP_HOST -e MYSQL_MCP_DB_PORT -e MYSQL_MCP_USERNAME \
  -e MYSQL_MCP_PASSWORD -e MYSQL_MCP_DATABASE \
  ghcr.io/futuretea/mysql-mcp-server:dev

# HTTP
docker run --rm \
  -e MYSQL_MCP_HOST -e MYSQL_MCP_DB_PORT -e MYSQL_MCP_USERNAME \
  -e MYSQL_MCP_PASSWORD -e MYSQL_MCP_DATABASE \
  -p 8080:8080 ghcr.io/futuretea/mysql-mcp-server:dev --port 8080 --listen 0.0.0.0
```

HTTP / SSE **无鉴权、无 TLS**。默认 `--listen 127.0.0.1`。仅在受信网络使用；对外暴露时请自行加反向代理与认证。

## 配置

优先级：**flags > 环境变量 > 配置文件 > 默认值**。

### 环境变量

| 变量 | 说明 | 默认 |
|------|------|------|
| `MYSQL_MCP_LOG_LEVEL` | 日志级别 | `info` |
| `MYSQL_MCP_PORT` | MCP HTTP 端口（`0` = stdio） | `0` |
| `MYSQL_MCP_LISTEN` | MCP HTTP 监听地址 | `127.0.0.1` |
| `MYSQL_MCP_SSE_BASE_URL` | 对外 SSE base URL | `""` |
| `MYSQL_MCP_HOST` | MySQL TCP 主机 | `127.0.0.1` |
| `MYSQL_MCP_DB_PORT` | MySQL TCP 端口；与 `MYSQL_MCP_PORT` 不同 | `3306` |
| `MYSQL_MCP_USERNAME` | 必填的 MySQL 用户名 | 无 |
| `MYSQL_MCP_PASSWORD` | 可选 MySQL 密码；不会写入日志或返回 | 无 |
| `MYSQL_MCP_DATABASE` | 可选默认 MySQL database | 无 |
| `MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE` | 只跳过启动时的危险权限拒绝 | `false` |

### 服务账号

进程连上 MySQL 之后、任何工具执行 SQL 之前，启动检查会读取当前账号的 `SHOW GRANTS`。不带 `FOR` 的 `SHOW GRANTS` 包含强制角色（mandatory roles）；`SHOW GRANTS FOR CURRENT_USER()` 不包含。这些角色以及显式授予的角色所继承的权限，会在任何工具执行 SQL 之前展开。账号具备下列任一权限时，进程以 `[mysql_unsafe_service_role]` 退出：

- `SUPER`、`FILE`、`PROCESS`、`SHUTDOWN`、`RELOAD`、`CREATE USER`、`CREATE TABLESPACE` 或 `PROXY`
- `CREATE ROLE`、`DROP ROLE` 或 `EXECUTE`
- `REPLICATION SLAVE` 或 `REPLICATION CLIENT`
- `SHOW_ROUTINE` 或 `RESOURCE_GROUP_USER`
- 全局 `SELECT`、`ALL` 或 `ALL PRIVILEGES`（`ON *.*`）
- `mysql` 系统库上的任何权限，包括 `mysql.user` 上的 `SELECT`
- 替代 `SUPER` 或用于管理服务器的 MySQL 8 动态权限，包括 `SYSTEM_VARIABLES_ADMIN`、`SESSION_VARIABLES_ADMIN`、`CONNECTION_ADMIN`、`TP_CONNECTION_ADMIN`、`SYSTEM_USER`、`ROLE_ADMIN`、`BINLOG_ADMIN`、`REPLICATION_SLAVE_ADMIN`、`SET_USER_ID`、`ALLOW_NONEXISTENT_DEFINER`、`BACKUP_ADMIN`、`CLONE_ADMIN`、`FLUSH_PRIVILEGES`、`FLUSH_TABLES`、`FIREWALL_ADMIN` 和 `VERSION_TOKEN_ADMIN`

`USAGE` 加上应用库上的 `SELECT` 使用默认配置即可启动，不需要打开开关。`SELECT ON *.*` 不能启动，因为该授权包含 `mysql` 权限表。

启动会给 TCP 连接和权限探测 5 秒。服务器在这段时间内没有响应就停止启动。`allow_unsafe_database_role` 不会延长这个时限。

`allow_unsafe_database_role` 默认为 `false`。只有在无法准备最小权限账号时，才在配置文件中设为 `true`，或导出 `MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE=true`。服务会打一条警告并跳过这次权限拒绝。连接失败、权限探测失败、启动超时以及其它启动检查仍然会阻止启动。查询仍在只读事务中执行，但只读事务约束不了 `FILE`、`SHUTDOWN`、`SUPER`、复制权限、`PROXY`、`EXECUTE` 和上面的动态权限。只对你掌控的 MySQL 实例启用。

stdio MCP 和 `tools call` 为了保持协议输出干净，日志只保留 error 级别。这条 opt-out 警告以 warn 级别发出，在默认 `info` 级别的 HTTP 模式中可见。

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
allow_unsafe_database_role: false
```

```bash
./bin/mysql-mcp-server mcp --config config.example.yaml --port 8080
```

## 目录结构

```
cmd/mcp-server/          # 入口
internal/cmd/            # cobra CLI（mcp / tools / version / completion）
internal/mysql/          # 连接、发现、查询边界和结果编码
pkg/core/                # config / logging / version
pkg/server/mcp/          # MCP 注册与 transport
pkg/server/http/         # HTTP / SSE / healthz
pkg/toolset/             # Toolset 接口与过滤
pkg/toolset/mysql/       # 六个 MySQL 工具和共享参数 decoder
```

## 运行限制

本切片有意不增加查询超时、行数限制、响应大小限制或并发预算。服务应只放在受信网络；HTTP 和 SSE 没有内置认证或 TLS 终止。

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

### 本地 MySQL E2E

真实 MySQL E2E 测试仅供本地按需运行，不会进入 GitHub Actions 或 `make ci`。它会自行启动并删除一个临时的 `mysql:8.0.46` 容器，然后验证编译后的 CLI 与 MCP stdio transport：

```bash
MYSQL_MCP_INTEGRATION=1 go test ./internal/mysql -run '^TestMySQL8046Integration$' -count=1
```

## 使用已发布产物

稳定发布标签使用 `vX.Y.Z`。npm 使用 `X.Y.Z`；Docker 使用 `vX.Y.Z`。

```bash
npx -y @futuretea/mysql-mcp-server@<version>
docker run --rm -i ghcr.io/futuretea/mysql-mcp-server:v<version>
```

两种方式都会启动 MCP server 的 `mcp` 命令。包名或镜像名之后的参数会转发给该命令。

支持 stdio 的 MCP 客户端可通过 npm 配置：

```json
{
  "mcpServers": {
    "mysql-mcp-server": {
      "command": "npx",
      "args": ["-y", "@futuretea/mysql-mcp-server@<version>"],
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

支持的发布平台记录在 `.github/ci/release-platforms.json`。

## License

[MIT](LICENSE)
