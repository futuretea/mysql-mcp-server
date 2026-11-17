# MySQL 只读 MCP Server

[English](README.md)

面向一条 MySQL 连接的 MCP（Model Context Protocol）服务端与 CLI。它提供元数据发现和严格只读查询。它不支持写入、管理、备份、复制，也不在同一进程里连接第二个目标。

集成测试使用镜像 `mysql:8.0.46`。调用 `mysql_get_server_info` 时执行 `SELECT VERSION(), @@version_comment, DATABASE()`。启动过程不读取 `VERSION()`，也不拒绝其它服务器版本。

## 快速开始

```bash
# 使用 .tool-versions 和 go.mod 中的 Go 1.25.10。
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

# 不经过 MCP 客户端，用 CLI 列出工具，或调用一个工具
./bin/mysql-mcp-server tools list
./bin/mysql-mcp-server tools call mysql_list_tables --params '{"database":"inventory"}'
./bin/mysql-mcp-server tools call mysql_query --params '{"sql":"SELECT 1"}'
```

`make build` 生成 `./bin/mysql-mcp-server`。`version` 打印一行：`mysql-mcp-server version=<version> commit=<commit> date=<date>`。没有发布 ldflags 的构建打印 `version=dev`。

`tools list` 和 `tools describe` 不打开 MySQL 连接。`mcp` 和 `tools call` 会打开。上面的环境变量指向可连接的账号时，`tools call mysql_query` 返回 JSON envelope。MySQL 没有在监听时，命令以状态 1 退出，并打印 `[mysql_connection_failed] connection failed`。

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
| `mysql_query` | `sql` | 单条查询的列和编码后的 rows。 |

成功调用使用带 `schema_version: 1` 的同一 JSON envelope。非 NULL 的查询单元格是 `{"encoding","value"}`。`encoding` 为 `integer`、`float`、`utf8` 或 `base64`。SQL `NULL` 是 JSON `null`。文档约定之外的驱动类型失败，错误是 `[mysql_result_encoding_failed] result encoding failed`。

`tools call` 在打开 MySQL 之前解析 JSON。`[]`、`null` 和 `"x"` 失败，错误以 `parse tool params JSON:` 开头。这不是 `[mysql_invalid_params]`。JSON 对象会继续。`tools call` 接着调用 `buildRuntimeToolsets`，它会连接并做启动探测，然后 `DecodeToolParams` 才检查字段。设置了 `MYSQL_MCP_USERNAME` 且 MySQL 没有在监听时，`{}` 返回 `[mysql_connection_failed] connection failed`，即使这个对象对 `mysql_query` 也是无效的。没有用户名时，`{}` 先打印 `mysql runtime configuration unavailable: MYSQL_MCP_USERNAME is required`，然后返回 `[mysql_invalid_params] invalid parameters`。

在 MCP 上，不是 JSON 对象的参数由传输处理函数返回 `[mysql_invalid_params] invalid parameters`，此时还没有进入工具函数。未知、缺失、空字符串和非字符串字段在工具处理函数里检查。在 CLI 上，这次字段检查发生在连接尝试之后。

`mysql_query` 只接受一条解析后的读查询。它拒绝多语句、非查询语句、`INTO`、锁定读、用户变量赋值和已阻断的会话/文件函数。通过后查询在只读事务中执行，并始终回滚。

这不能证明存储函数、UDF、视图或插件没有副作用。请使用最小权限账号。除非设置 `allow_unsafe_database_role`，启动时会拒绝 `EXECUTE`、库级 `ALL`、`FILE`、`SHOW_ROUTINE`、`mysql` 系统库上的授权、全局 `SELECT` 以及其它管理权限。见[服务账号](#服务账号)。

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
# healthy
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

`make docker` 构建本地镜像，并打上标签 `ghcr.io/futuretea/mysql-mcp-server:dev` 和 `ghcr.io/futuretea/mysql-mcp-server:<git describe>`。`:dev` 只存在于执行构建的机器上。GHCR 不发布 `:dev`。

```bash
make docker

# Stdio。镜像 ENTRYPOINT 是 `mysql-mcp-server mcp`。
docker run -i --rm \
  -e MYSQL_MCP_HOST -e MYSQL_MCP_DB_PORT -e MYSQL_MCP_USERNAME \
  -e MYSQL_MCP_PASSWORD -e MYSQL_MCP_DATABASE \
  ghcr.io/futuretea/mysql-mcp-server:dev

# HTTP。只把宿主机端口发布到 loopback。
docker run --rm \
  -e MYSQL_MCP_HOST -e MYSQL_MCP_DB_PORT -e MYSQL_MCP_USERNAME \
  -e MYSQL_MCP_PASSWORD -e MYSQL_MCP_DATABASE \
  -p 127.0.0.1:8080:8080 \
  ghcr.io/futuretea/mysql-mcp-server:dev --port 8080 --listen 0.0.0.0
```

`curl -s http://127.0.0.1:8080/healthz` 打印 `healthy`。

已发布的镜像标签见[使用已发布产物](#使用已发布产物)。

HTTP 和 SSE 没有认证，也没有 TLS。`--listen` 的默认值是 `127.0.0.1`。监听地址不是 loopback 时，放在反向代理后面。

## 配置

连接设置和服务器设置的加载方式不同。

### MySQL 连接

服务器只从下列环境变量读取连接。flag、YAML 键和 `MYSQL_DSN` 都不能设置主机、端口、用户、密码或 database。

| 变量 | 说明 | 默认 |
|------|------|------|
| `MYSQL_MCP_HOST` | MySQL TCP 主机 | `127.0.0.1` |
| `MYSQL_MCP_DB_PORT` | MySQL TCP 端口。这不是 HTTP 端口。 | `3306` |
| `MYSQL_MCP_USERNAME` | MySQL 用户 | 无 |
| `MYSQL_MCP_PASSWORD` | MySQL 密码。服务器不把它写入日志，也不把它复制进错误文本。 | 空 |
| `MYSQL_MCP_DATABASE` | 默认 database | 无 |

工具查询之前必须设置 `MYSQL_MCP_USERNAME`。它为空时，`mcp` 仍会启动，并向 stderr 写入这一行：

```text
mysql runtime configuration unavailable: MYSQL_MCP_USERNAME is required
```

字段有效的工具调用随后返回 `[mysql_configuration_unavailable] configuration unavailable`。`tools list` 和 `tools describe` 不读取这些变量。

地址拒绝连接或不可达时，返回 `[mysql_connection_failed] connection failed`。连接成功之前不会做权限检查。

### 服务器设置

`--port`、`--listen`、`--sse-base-url` 和 `--log-level` 只存在于 `mysql-mcp-server mcp`。`tools` 会拒绝这些 flag。在 `mcp` 上，你传入的 flag 优先。非空环境变量其次。`AutomaticEnv` 没有调用 `AllowEmptyEnv`，所以空的 `MYSQL_MCP_PORT` 或 `MYSQL_MCP_LOG_LEVEL` 会留下 YAML 里的值。然后使用 YAML 文件。最后使用默认值。没有 `MYSQL_MCP_` 前缀的名字，例如 `MCP_PORT`，会被忽略。

| 设置 | `mcp` flag | 环境变量 | YAML 键 | 默认 |
|---|---|---|---|---|
| HTTP 端口。`0` 保持 stdio。 | `--port` | `MYSQL_MCP_PORT` | `port` | `0` |
| HTTP 监听主机 | `--listen` | `MYSQL_MCP_LISTEN` | `listen` | `127.0.0.1` |
| 对外 SSE base URL | `--sse-base-url` | `MYSQL_MCP_SSE_BASE_URL` | `sse_base_url` | 空 |
| 日志级别 | `--log-level` | `MYSQL_MCP_LOG_LEVEL` | `log_level` | `info` |

`tools` 从 `MYSQL_MCP_LOG_LEVEL` 或 YAML 的 `log_level` 读取日志级别。它没有 `--log-level` flag。日志级别是 `trace`、`debug`、`info`、`warn`、`error`、`fatal`、`panic` 和 `disabled`。stdio 模式和 `tools call` 会把低于 `error` 的级别提升到 `error`，因此那里看不到危险角色警告。HTTP 模式使用你设置的级别。默认 `info` 时，HTTP 模式会显示这条警告。

`--config`、`--enabled-tools`、`--disabled-tools`、`--enable-domains` 和 `--disable-domains` 是持久 flag。它们在 `mcp` 和 `tools` 上都有效。你传入的 flag 优先。非空环境变量其次。空的 `MYSQL_MCP_ENABLED_TOOLS` 或 `MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE` 会留下 YAML 里的值，包括 `allow_unsafe_database_role: true`。然后使用 YAML 文件。最后使用默认值。工具或 domain 列表有条目，但去掉空白后没有名字时，加载会被拒绝。`MYSQL_MCP_DISABLED_TOOLS` 只有空格时，不会清掉 YAML 里的禁用列表。空环境变量仍保留 YAML 的值。`--disabled-tools=` 是空 flag，仍会替换该值。

| 设置 | Flag | 环境变量 | YAML 键 | 默认 |
|---|---|---|---|---|
| 要启用的工具 | `--enabled-tools` | `MYSQL_MCP_ENABLED_TOOLS` | `enabled_tools` | 全部工具 |
| 要禁用的工具 | `--disabled-tools` | `MYSQL_MCP_DISABLED_TOOLS` | `disabled_tools` | 无 |
| 要启用的 domain | `--enable-domains` | `MYSQL_MCP_ENABLED_DOMAINS` | `enabled_domains` | 全部 domain |
| 要禁用的 domain | `--disable-domains` | `MYSQL_MCP_DISABLED_DOMAINS` | `disabled_domains` | 无 |
| 只跳过危险权限拒绝 | 无 | `MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE` | `allow_unsafe_database_role` | `false` |

工具和 domain 列表用逗号分隔。工具 domain 是 `mysql`。

```bash
./bin/mysql-mcp-server tools list --enabled-tools mysql_query,mysql_list_databases
MYSQL_MCP_ENABLED_TOOLS=mysql_query ./bin/mysql-mcp-server tools list
```

第一条命令打印 `mysql_query` 和 `mysql_list_databases`。第二条命令打印 `mysql_query`。

`MYSQL_MCP_MASKING_JSON` 不在上表中。它使用 `os.LookupEnv`，不使用空变量规则。变量存在且为空时，即使 YAML 里的 `masking` 对象有效，加载配置也会失败，错误是 `load config: invalid masking config`。变量未设置时，保留 YAML 对象。见[输出脱敏](#输出脱敏)。

### 服务账号

进程连上 MySQL 之后、任何工具执行 SQL 之前，启动检查会读取当前账号的 `SHOW GRANTS`。不带 `FOR` 的 `SHOW GRANTS` 包含强制角色（mandatory roles）；`SHOW GRANTS FOR CURRENT_USER()` 不包含。这些角色以及显式授予的角色所继承的权限，会在任何工具执行 SQL 之前展开。账号具备下列任一权限时，进程以 `[mysql_unsafe_service_role]` 退出：

- `SUPER`、`FILE`、`PROCESS`、`SHUTDOWN`、`RELOAD`、`CREATE USER`、`CREATE TABLESPACE` 或 `PROXY`
- `CREATE ROLE`、`DROP ROLE` 或 `EXECUTE`
- `REPLICATION SLAVE` 或 `REPLICATION CLIENT`
- `SHOW_ROUTINE` 或 `RESOURCE_GROUP_USER`
- 全局 `SELECT`、`ALL` 或 `ALL PRIVILEGES`（`ON *.*`）
- 库级 `ALL` 或 `ALL PRIVILEGES`（`ON db.*`）。MySQL 把 `EXECUTE` 存在这条数据库权限记录里，`SHOW GRANTS` 仍显示缩写，语句里不会出现 `EXECUTE`。单表上的 `ALL`（`ON db.table`）不包含 `EXECUTE`
- `mysql` 系统库上的任何权限，包括 `mysql.user` 上的 `SELECT`
- 替代 `SUPER` 或用于管理服务器的 MySQL 8 动态权限，包括 `SYSTEM_VARIABLES_ADMIN`、`SESSION_VARIABLES_ADMIN`、`CONNECTION_ADMIN`、`TP_CONNECTION_ADMIN`、`SYSTEM_USER`、`ROLE_ADMIN`、`BINLOG_ADMIN`、`REPLICATION_SLAVE_ADMIN`、`SET_USER_ID`、`ALLOW_NONEXISTENT_DEFINER`、`BACKUP_ADMIN`、`CLONE_ADMIN`、`FLUSH_PRIVILEGES`、`FLUSH_TABLES`、`FIREWALL_ADMIN`、`OPTIMIZE_LOCAL_TABLE` 和 `VERSION_TOKEN_ADMIN`

`USAGE` 加上应用库上的 `SELECT` 使用默认配置即可启动，不需要打开开关。`SELECT ON *.*` 不能启动，因为该授权包含 `mysql` 权限表。不含 `EXECUTE` 的展开数据库权限列表也可以启动，随后调用例程会在 MySQL 里失败，而不是在启动时失败。

DBA 用 `SQL SECURITY DEFINER` 创建的视图属于已发布的 schema。能 `SELECT` 该视图的账号可以读到它的输出，包括 `mysql.user` 的计数或函数结果，即使自己没有 `EXECUTE`。启动检查不判断视图正文。对该账号而言 `VIEW_DEFINITION` 为空，`SHOW CREATE VIEW` 也被拒绝，因此检查无法区分普通的默认 DEFINER 应用视图和发布了目录数据的视图。不要发布服务账号不该看到的数据的 DEFINER 视图。这条边界不能代替对 `EXECUTE` 或库级 `ALL` 的拒绝。

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
pkg/core/                # config、logging、version
pkg/core/masking/        # 输出脱敏策略
pkg/server/mcp/          # MCP 注册与 transport
pkg/server/http/         # HTTP / SSE / healthz
pkg/toolset/             # Toolset 接口与过滤
pkg/toolset/mysql/       # 六个 MySQL 工具和共享参数 decoder
```

## 输出脱敏

未配置 `masking`、省略 `enabled`，或 `enabled` 为 false 时，脱敏关闭。工具结果保持现有 JSON。将 `enabled` 设为 true 且至少配置一条规则后，才会脱敏。环境变量 `MYSQL_MCP_MASKING_JSON` 一旦存在，就整份替换 YAML 中的对象。空值会被拒绝。未设置该变量时，仍使用 YAML 对象。

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

名称规则匹配完整的列名、表名、索引名、库名，以及解码后的 JSON 对象键。模式会先单独解析再锚定到整个名称，因此 `a)|(b` 这种片段会被拒绝，而不会去匹配前缀或后缀。值模式使用 RE2 的最左优先匹配，不会再寻找更长的分支。匹配到的字符串替换为占位符。原来是文本或 base64 的 MySQL 单元格仍保留 `{"encoding","value"}`。匹配到的数字或其他非字符串变成 JSON `null`。本来就是 SQL `NULL` 的值仍是 `null`。因此结果里的 JSON `null` 可能是数据库 NULL，也可能是被隐去的值，JSON 类型也可能改变。

值规则对原始文本做子串匹配，包括单字符字面量。替换只做一次，不会再搜索占位符。任一分支可以不消耗字符就匹配的模式会被拒绝，例如词边界分支。utf8 单元格里的 JSON 对象和数组会按 JSON 读取，包括转义后的键。值规则命中 JSON 数字、布尔值，或 MySQL 整数、浮点单元格时，整个值变成 null，不会改数字中间的字符。未命中的数字保留原始文本。标准 base64 单元格会先解码、再脱敏、再编码，无效 UTF-8 旁边的 ASCII 密钥也会被值模式命中。看起来像 base64 的普通文本不会被解码。这不是对所有隐私、压缩数据或任意编码的识别。

错误的脱敏文档在加载配置时失败，此时还没有工具运行。`names`、`name_patterns`、`values` 和 `value_patterns` 各自最多 32 条。名称最多 128 字节。值字面量最多 256 字节。模式最多 128 字节。占位符必须是 1 到 64 字节。`enabled: true` 但没有任何规则时同样失败。命令打印 `load config: invalid masking config`，并且不会回显文档。

脱敏开启时，一次工具结果共用一个预算。该预算累计节点数，也累计传给 `Budget.add` 的字节数。文本替换计入 `outputSize` 算出的输出大小。重新编码的 JSON 计入编码后的长度。数字或布尔检查计入原始文本长度。预算允许 10,000 个节点，以及这些计入字节合计 1 MiB。另外，单条输入字符串超过 1 MiB 会被拒绝。字符串内部的 JSON 会脱敏 2 轮。再往里的结构化 JSON 对象或数组会使这次结果失败。JSON 深度超过 32 层会使结果失败。一个值里的匹配超过 4,096 个会使结果失败。调用返回 `[mysql_masking_failed] result was not returned`，并且没有部分结果。

`schema_version` 和工具名是服务器固定字段，不参与脱敏。固定的公开错误文本不会被改写，所以一个很短的字面量如果也出现在错误句子或占位符里，仍可能看见它。保证的范围是：已配置的原始数据不会留在覆盖到的结果文本里。

JSON 重复键、两个名称脱敏后冲突，或不支持的单元格，也会返回 `[mysql_masking_failed] result was not returned`。发往数据库的命令不变。

HTTP 调试日志记录路由类别（`mcp`、`sse`、`message` 或 `other`），不记录原始路径或查询字符串。脱敏过程不会把源值写入日志。

## 运行限制

查询执行没有超时、行数限制、响应大小限制或并发预算。上面的脱敏界限只在脱敏开启时生效。HTTP 和 SSE 没有认证，也没有 TLS。监听地址不是 loopback 时，放在受信网络上。

### 服务器返回的错误

这些字符串是固定的。它们不包含 SQL 文本、密码或脱敏文档。

| 文本 | 时机 |
|---|---|
| `[mysql_configuration_unavailable] configuration unavailable` | 连接环境缺失，或服务没有创建。 |
| `[mysql_invalid_params] invalid parameters` | 在 MCP 上，传输处理函数对非对象参数返回这段文本，此时还没有进入工具函数。工具处理函数对未知、缺失、空或非字符串字段返回同一段文本。`tools call` 只有在已经尝试连接之后才会做这次字段检查。 |
| `[mysql_connection_failed] connection failed` | 驱动错误是截止时间、`driver.ErrBadConn`、`sql.ErrConnDone`，或 `net.Error`。 |
| `[mysql_unsafe_service_role] database service role is not permitted` | 账号有被拒绝的权限，且没有打开豁免。 |
| `[mysql_query_rejected] query rejected` | `mysql_query` 拒绝这段 SQL。 |
| `[mysql_database_error] database error` | MySQL 返回其它错误，包括不属于上面连接失败条件的 `SHOW GRANTS` 失败。驱动原文不会被复制。 |
| `[mysql_result_encoding_failed] result encoding failed` | 单元格类型不在文档约定的编码里。 |
| `[mysql_masking_failed] result was not returned` | 脱敏无法在界限内完成。 |
| `load config: invalid masking config` | 加载配置时拒绝脱敏文档。文档不会被回显。 |

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

### 使用 MySQL 的测试

`make ci` 运行 lint、`go test ./...` 和构建。除非设置 `MYSQL_MCP_INTEGRATION=1`，MySQL 集成测试会跳过，所以 `make ci` 不会启动 MySQL。

GitHub Actions 作业 `MySQL integration` 设置 `MYSQL_MCP_INTEGRATION=1`，并运行 `TestMySQL8046Integration` 和 `TestStartupPrivilegesIntegration`。每个测试都会自行启动并删除 `mysql:8.0.46` 容器。`TestMySQL8046Integration` 检查查询行为。它要求 `mysql_get_server_info` 返回的版本字符串以 `8.0.46` 开头。启动过程不读取也不强制服务器版本。

本机 Docker 能拉取该镜像时，运行同一组测试：

```bash
MYSQL_MCP_INTEGRATION=1 go test ./internal/mysql -count=1 -timeout 12m -run 'TestMySQL8046Integration|TestStartupPrivilegesIntegration'
```

## 使用已发布产物

当前已发布的最新版本是 `0.2.0`。稳定发布标签使用 `vX.Y.Z`。npm 使用 `X.Y.Z`。Docker 使用 `vX.Y.Z`。这些标签的 GitHub Release 页面不附带二进制文件。用 npm 或 Docker 安装。

```bash
npx -y @futuretea/mysql-mcp-server@0.2.0
docker run --rm -i \
  -e MYSQL_MCP_HOST -e MYSQL_MCP_DB_PORT -e MYSQL_MCP_USERNAME \
  -e MYSQL_MCP_PASSWORD -e MYSQL_MCP_DATABASE \
  ghcr.io/futuretea/mysql-mcp-server:v0.2.0
```

`ghcr.io/futuretea/mysql-mcp-server:latest` 指向同一版本。更早的已发布版本仍然存在：npm `0.0.2` 和 `0.1.0`，Docker `v0.0.2` 和 `v0.1.0`。`0.2.0` 是第一个包含输出脱敏的版本。没有 npm `0.0.1`，也没有 GHCR `v0.0.1` 或 `:dev`。

npm 包 `@futuretea/mysql-mcp-server@0.2.0` 里仍然是该版本发布时的 README。本仓库里的 README 才是改正后的文本。替换已发布 tarball 里的 README 需要一次新发布，这次改动不会发布。

两条命令都启动 `mysql-mcp-server mcp`。包名或镜像名之后的参数是 `mcp` 的 flag，例如 `--port 8080`。`npx` 不能运行 `tools` 或 `version`。这些子命令请用 `make build` 得到的二进制。

npm 发布 `darwin/amd64`、`darwin/arm64`、`linux/amd64`、`linux/arm64` 和 `windows/amd64`。`v0.2.0` 的 Docker 镜像索引包含 `linux/amd64` 和 `linux/arm64`。同一份清单在 `.github/ci/release-platforms.json`。

支持 stdio 的 MCP 客户端可通过 npm 配置：

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
