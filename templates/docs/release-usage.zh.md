
## 使用已发布产物

稳定发布标签使用 `vX.Y.Z`。npm 使用 `X.Y.Z`；Docker 使用 `vX.Y.Z`。

```bash
npx -y <npm-package>@<version>
docker run --rm -i <image>:v<version>
```

两种方式都会启动 MCP server 的 `mcp` 命令。包名或镜像名之后的参数会转发给该命令。

支持 stdio 的 MCP 客户端可通过 npm 配置：

```json
{
  "mcpServers": {
    "server": {
      "command": "npx",
      "args": ["-y", "<npm-package>@<version>"]
    }
  }
}
```

支持的发布平台记录在 `.github/ci/release-platforms.json`。
