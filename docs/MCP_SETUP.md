# MCP Setup Guide

Recall exposes terminal session history to AI tools via the [Model Context Protocol](https://modelcontextprotocol.io/) over a **local Unix domain socket**. No TCP ports are opened.

## Socket location

```
~/.recall/mcp.sock
```

On Windows (Go 1.22+), this resolves to:

```
%USERPROFILE%\.recall\mcp.sock
```

The MCP server starts automatically when you run `recall` and stops when the session exits.

## Available tools

| Tool | Description |
|------|-------------|
| `list_sessions` | Browse captured terminal sessions (`limit`, default 10) |
| `get_session_context` | Retrieve session history slice (`session_id`, `token_budget`) — full slicing ships in Week 4 |
| `get_latest_error` | Pull the 61-line window around the most recent error (`session_id` optional) |

## Claude Desktop configuration

Claude Desktop speaks MCP over **stdio**, while Recall serves MCP over a **Unix socket**. Use the `recall-bridge` helper to connect them:

### 1. Build the binaries

```bash
go build -o recall ./cmd/recall
go build -o recall-bridge ./cmd/recall-bridge
```

### 2. Start a Recall terminal session

In one terminal:

```bash
./recall
```

This starts the PTY wrapper **and** the background MCP socket server.

### 3. Add to Claude Desktop config

Edit `claude_desktop_config.json`:

**macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`  
**Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "recall": {
      "command": "/absolute/path/to/recall-bridge",
      "args": []
    }
  }
}
```

Replace `/absolute/path/to/recall-bridge` with the full path to your built bridge binary.

### 4. Restart Claude Desktop

After restarting, Claude can call `list_sessions`, `get_latest_error`, and `get_session_context` against your live terminal capture.

## Verifying the socket

With `recall` running, confirm the socket exists:

```bash
ls -la ~/.recall/mcp.sock
```

## Security notes

- Recall binds **only** to a local Unix domain socket — never to `127.0.0.1` TCP.
- All data stays on disk at `~/.recall/recall.db` with zero external telemetry.
- Week 4 adds prompt-injection boundaries and secret redaction on MCP output.
