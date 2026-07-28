
## Use released artifacts

Release tags use `vX.Y.Z`. Use `X.Y.Z` for npm and `vX.Y.Z` for Docker.

```bash
npx -y <npm-package>@<version>
docker run --rm -i <image>:v<version>
```

Both launch the MCP server's `mcp` command. Arguments after the package or image are forwarded to that command.

Configure an MCP client that supports stdio with npm:

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

Supported release platforms are recorded in `.github/ci/release-platforms.json`.
