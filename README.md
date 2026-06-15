# Recall (v1)

**A local-first, zero-telemetry PTY-wrapping developer daemon that turns your terminal into a queryable context layer for AI models via MCP.**

---

## The Problem: The Copy-Paste Friction Loop

When developers debug using AI assistants (like Claude, Cursor, or ChatGPT), their source code is usually accessible via the editor. However, the **runtime context**—compilation errors, test failures, panic trace blocks, database logs, and server outputs—remains trapped inside the terminal pane. 

This introduces the **Context Gap**:
1. You run a command (e.g., `go test` or `npm run dev`) and it crashes.
2. You manually scroll up, highlight the stack trace, copy it, and paste it into the AI chat box.
3. You manually describe what command you ran and what happened.
4. You iterate, repeating this loop dozens of times a day.

**Recall** bridges this gap. By running as a transparent, high-performance pseudoterminal (PTY) wrapper, it passively captures, cleans, and indexes your terminal activity in a local SQLite database, serving it directly to your AI tools via a local [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) daemon. No more highlighting. No more manual copy-pasting.

---

## Architectural Topology & How it Works

Recall is engineered to introduce **sub-microsecond, zero-perceived latency** to your typing experience. It uses a low-level PTY wrapper and a non-blocking ingestion pipeline to ensure that your interactive shell remains snappy, even under heavy compiler output streams.

```
                         +-----------------------------+
                         |     Interactive Shell       |
                         | ($SHELL e.g. bash/zsh/fish) |
                         +--------------+--------------+
                                        |
                                        | Spawns & Intercepts
                                        v
                         +-----------------------------+
                         |       Unix PTY Engine       |
                         |    (creack/pty wrapper)     |
                         +--------------+--------------+
                                        |
                       +----------------+----------------+
                       | io.TeeReader                    |
                       v Zero-Latency                    v Background
             +------------------+              +-------------------+
             |  Native os.Stdout|              |  Ingest Pipeline  |
             | (Instant Render) |              | (stripansi loop)  |
             +------------------+              +---------+---------+
                                                         |
                                                         v
                                               +-------------------+
                                               |  Secret Redactor  |
                                               | (In-memory scrub) |
                                               +---------+---------+
                                                         |
                                                         v
                                               +-------------------+
                                               |   Error Tagging   |
                                               | (Regex Classifier)|
                                               +---------+---------+
                                                         |
                                                         v
                                               +-------------------+
                                               | Ring Buffer Queue |
                                               | (Non-blocking ch) |
                                               +---------+---------+
                                                         |
                                                         v Batched Flush
                                               +-------------------+
                                               | SQLite DB (WAL)   |
                                               | (~/.recall/db)    |
                                               +---------+---------+
                                                         ^ Reads Logs
                                                         |
                                               +---------+---------+
                                               | Local MCP Daemon  |
                                               | (~/.recall/socket)|
                                               +---------+---------+
                                                         ^ JSON-RPC 2.0
                                                         |
                                               +---------+---------+
                                               |     AI Client     |
                                               | (Cursor / Claude) |
                                               +-------------------+
```

### Ingestion Flow:
1. **Low-Latency Terminal Mirroring:** Using Go's low-level `io.TeeReader`, standard terminal inputs/outputs are written synchronously to `os.Stdout`. The user experiences zero typing latency or redraw delay.
2. **Style Code Cleansing:** The duplicate stream runs asynchronously through a fast ANSI stripper (`github.com/acarl005/stripansi`) to scrub out color/formatting sequences.
3. **Secret Redaction:** High-risk credentials (AWS keys, GitHub tokens, passwords) are sanitized in-memory using highly efficient regex compilation.
4. **Classification & Error Tagging:** Clean text is evaluated against language-specific panic/traceback patterns (Go, Python, Node.js, Shell) to mark lines as error events.
5. **Asynchronous Batched Storage:** Logs are pushed to an in-memory channel and committed to a local, WAL-optimized SQLite instance in batches of 50 rows or every 100 milliseconds to avoid disk thrashing.

---

## Go Package Topology

The repository is structured as a modular, CGO-free Go application:

```
├── cmd/
│   ├── recall/             # CLI application entry point (Cobra commands)
│   └── recall-bridge/      # Relays stdio JSON-RPC traffic to the local Unix domain socket
├── internal/
│   ├── ingest/             # Stream cleansing, secret redaction, and batched flushing pipeline
│   ├── mcp/                # MCP server definition, JSON-RPC schema, and tools registration
│   ├── project/            # Workspace parent directory auto-discovery logic (resolving Git repository name)
│   ├── pty/                # Unix pseudoterminal lifecycle wrapper using creack/pty
│   └── storage/            # Database storage operations, WAL configuration, and SQL query definitions
```

---

## Core Capabilities & Built-in Guardrails

### 1. The 3-Tool MCP Surface
Recall exposes three highly target-specific JSON-RPC 2.0 tools to AI models:

*   **`list_sessions`**  
    *Description:* Allows the AI to browse the history of captured terminal sessions.  
    *Behavior:* Returns a lightweight JSON catalog of recent terminal workspaces, capturing project working directories, launch timestamps, and status flags.
*   **`get_session_context`**  
    *Description:* Retrieves a context-sliced chunk of terminal history for a specific session.  
    *Behavior:* Rather than feeding the entire log history to the model, it applies a **Token-budget Slicing Algorithm** (1 token ≈ 4 characters). It extracts the first **25 lines** (initialization context) and the last **150 lines** (most recent execution state), stitching them with a clear truncation delimiter: `[... TEXT TRUNCATED BY RECALL ENGINE ...]`. The engine dynamically shrinks lines if the budget is breached.
*   **`get_latest_error`**  
    *Description:* Instantly pulls the exact timeline surrounding the most recent runtime crash.  
    *Behavior:* Finds the highest log-line primary key tagged with `is_error = 1`. It then fetches a symmetric boundary window spanning **30 lines before** and **30 lines after** the failure, providing a precise 61-line runtime snapshot (execution frame, traceback details, and terminal tail output).

### 2. Interactive App Ignoring (Anti-Pollution)
Recall scans `xterm` alternate screen control codes to dynamically identify when fullscreen interactive visual applications are launched.
*   When software like `vim`, `nano`, `less`, `htop`, or similar visual apps activate, the ingestion pipeline **halts database commits** for their duration.
*   This prevents screen-redraw garbage sequences and static terminal frames from polluting your structured database logs.

### 3. Memory & Backpressure Protection
To prevent terminal commands with massive stdout loops (like `cat /dev/urandom` or raw build streams) from overwhelming system resources:
*   The ingestion pipeline uses a fixed-capacity **ring channel pool** (512 lines capacity).
*   If the database flusher cannot keep up under extreme backpressure, the channel drops incoming rows.
*   This bounds the daemon’s maximum memory footprint below **45 Megabytes**, prioritizing system stability over long-term log completeness.

### 4. Hardcoded In-Memory Redaction Pass
To guarantee credentials do not end up in SQLite database files:
*   **AWS Access Keys:** Strips strings matching `\b(AKIA|ASIA)[A-Z0-9]{16}\b`.
*   **GitHub Tokens:** Strips strings matching `\bghp_[a-zA-Z0-9]{36}\b`.
*   **Generic Assignments:** Cleans patterns like `password = "secret"`, `API_KEY: "token"`, etc.
*   *Implementation:* Substituted in-memory with `[REDACTED_SECRET]` *before* database serialization.

### 5. Absolute Privacy Posture
*   **Local-First, Local-Only:** Communicates strictly via local Unix Domain Sockets (`~/.recall/mcp.sock`).
*   **Zero Egress:** Contains no HTTP/HTTPS clients, telemetry dependencies, or remote auto-update hooks.
*   **Air-Gapped Safety:** What happens in your terminal stays completely on your disk.

---

## Onboarding & CLI Quickstart

### 1. Build and Install
Compile the Recall binary and standard stdio-to-socket bridge:

```bash
# Compile optimized, stripped binaries
go build -ldflags="-s -w" -o recall ./cmd/recall
go build -ldflags="-s -w" -o recall-bridge ./cmd/recall-bridge

# Move them to your path
mv recall recall-bridge /usr/local/bin/
```

### 2. Drop into a Tracked Session
Simply launch your terminal shell wrapped in Recall:

```bash
recall
```
This spawns your default `$SHELL` (or fallback) in a PTY environment. Behind the scenes, it opens the database, starts the background MCP socket server, and begins logging. Any code compilation, script run, or program error is now actively captured.

To exit the tracked session, simply run `exit` or press `Ctrl+D`.

### 3. CLI Administration

*   **`recall doctor`**  
    Runs diagnostic and filesystem validation tests. It confirms database write permissions on `~/.recall/recall.db`, tests Unix socket lifecycle binding on `~/.recall/mcp.sock`, and verifies environment configurations.
*   **`recall purge --all`**  
    Deletes all logged terminal sessions and lines, and executes a database `VACUUM;` to reclaim disk space immediately.

---

## MCP AI Client Integration

Since standard AI clients (like Claude Desktop) expect a `stdio` transport interface, Recall includes a lightweight utility—`recall-bridge`—to bridge stdio JSON-RPC traffic directly to the active Unix domain socket.

### Claude Desktop Configuration
Add the following profile block to your `claude_desktop_config.json` (typically located at `~/Library/Application Support/Claude/claude_desktop_config.json` on macOS or `~/.config/Claude/claude_desktop_config.json` on Linux):

```json
{
  "mcpServers": {
    "recall": {
      "command": "/usr/local/bin/recall-bridge",
      "args": []
    }
  }
}
```

### Cursor Integration
1. Open Cursor Settings -> **Features** -> **MCP**.
2. Click **+ Add New MCP Server**.
3. Configure:
   *   **Name:** `Recall`
   *   **Type:** `command`
   *   **Command:** `/usr/local/bin/recall-bridge`
4. Save and ensure the status bubble turns green.

*Note: For `recall-bridge` to connect, you must have an active `recall` terminal wrapper session running in your terminal.*
