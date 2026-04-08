# legacy-map

Automatic execution flow mapping for PHP/Symfony via XDebug + AI.

> You click in your app. You ask Claude: "What just happened?" He tells you.

## The problem

On a PHP/Symfony codebase — especially legacy:
- Nobody knows exactly what the code does
- Documentation doesn't exist or is outdated
- Devs spend hours navigating code to understand a single flow
- There's dead code nobody dares to touch

## How it works

```
You trigger a request in your app
        │
   XDebug captures the full execution trace
        │
   legacy-map filters the noise (99.97% of framework calls eliminated)
        │
   You ask Claude → he explains the flow, generates docs, diagrams
```

Works without AI too — `parse` and `watch` output JSON directly, no LLM required.

## Real-world results

| Endpoint | Raw calls | After filtering | Reduction |
|----------|-----------|-----------------|-----------|
| POST /api/admin/clients | 133,000 | 43 | 99.97% |
| POST /api/auth/login | 102,272 | 29 | 99.97% |
| GET /api/admin/cases | 133,000 | 43 | 99.97% |
| POST /api/chat | 7,432 | 16 | 99.78% |

## Quick Start

### 1. Install

```bash
go install github.com/drkilla/legacy-map@latest
```

### 2. Setup in your project

```bash
cd /your/symfony/project
legacy-map init
# Follow the displayed instructions
```

`init` detects your environment (Docker or local), checks if XDebug is installed, generates the config, and guides you step by step.

### 3. Connect Claude Code

```bash
legacy-map setup-mcp ./xdebug-traces
```

### 4. Use

Restart Claude Code, then:

```
> "Trace the flow of POST /api/clients"
```

Claude triggers the request, captures the XDebug trace, and explains the full execution flow. No browser, no curl.

## The killer feature: trigger_trace

No need to leave Claude Code. Just ask:

```
> "What happens when I POST to /api/auth/login?"
> "Trace the GET /api/cases flow with this token: Bearer xxx"
> "Compare the flows of /login and /clients"
```

Claude uses the `trigger_trace` MCP tool to fire the HTTP request with XDebug tracing, capture the trace, filter it, and explain the result — all in one question.

## Commands

```bash
legacy-map init                    # Setup XDebug + trace directory + install guide
legacy-map parse <file.xt>         # Parse a trace → JSON stdout
legacy-map watch <dir>             # Watch and parse continuously
legacy-map serve <dir>             # Watcher + MCP server (for Claude Code)
legacy-map setup-mcp <dir>         # Configure Claude Code automatically
```

### Options

```bash
# Change application namespace (default: App\)
legacy-map parse --app-ns=Acme\\ trace.xt

# Exclude additional namespaces
legacy-map parse --exclude-ns=Sentry\\,Jean85\\ trace.xt

# Pretty-print JSON (default: compact)
legacy-map parse --pretty trace.xt

# Control return values: truncate (default), type (type only), none (omit)
legacy-map parse --returns=type trace.xt
legacy-map parse --returns=none trace.xt

# Disable trivial call collapsing (getters/setters/hydrations)
legacy-map parse --no-collapse trace.xt

# HTTP timeout for slow endpoints (default: 30s)
legacy-map serve --http-timeout=60 ./xdebug-traces
```

### Output optimization

On large traces (1000+ nodes), the default settings optimize for LLM consumption:
- **Compact JSON** — ~50% smaller than pretty-printed
- **Trivial call collapse** — getters, setters, `toDomain()`, `toArray()` with <1ms and no children are grouped into a single summary node
- **MCP server defaults** — uses `--returns=type` automatically (shows `CustomerModel` instead of the full object dump)

## MCP Tools

| Tool | Description |
|------|-------------|
| `trigger_trace` | Trigger an HTTP request with XDebug and return the filtered call tree |
| `list_traces` | List traces in memory with metadata |
| `get_last_trace` | Return the N most recent complete traces |
| `get_trace_by_uri` | Find traces by URI pattern |

## What the filtering eliminates

legacy-map applies 3 layers of filtering:

1. **PHP internal functions** — `strlen`, `array_map`, `sprintf`... eliminated
2. **Framework namespaces** — `Symfony\`, `Doctrine\`, `Twig\`, `Monolog\`... excluded
3. **Vendor collapse** — when your code calls `EntityManager::persist()`, you see the call but not Doctrine's 200 internal lines

Result: on a typical Symfony request, 99.97% of calls are framework noise. legacy-map keeps only your application code.

## Documentation generation

With captured traces, ask Claude to generate docs:

```
> "Generate technical documentation for all captured flows
>  with Mermaid sequenceDiagram diagrams"
```

Claude produces for each endpoint:
- A business summary (readable by non-devs)
- A Mermaid sequence diagram
- The list of services involved with their roles
- External dependencies (Doctrine, Mailer, etc.)
- Observations: perf bottlenecks, N+1, dead code

## Troubleshooting

### No traces appearing

**XDebug not installed:**
```bash
php -m | grep xdebug
# Docker: docker compose exec php php -m | grep xdebug
```
If absent, `legacy-map init` guides you through installation.

**XDebug installed but not tracing:**
```bash
# Check config
php -i | grep xdebug.mode
# Should show: trace

php -i | grep xdebug.start_with_request
# If "trigger": add ?XDEBUG_TRACE=1 to the URL
# If "yes": every request is traced automatically
```

**Trace directory not accessible:**
```bash
# Check the directory exists and is writable
ls -la /tmp/xdebug-traces/

# Docker: check the volume is mounted
docker compose exec php ls -la /tmp/xdebug-traces/
```

### Traces are .xt.gz files (compressed)

XDebug 3.x compresses by default. legacy-map cannot read compressed files.

```ini
# Add to your XDebug config:
xdebug.use_compression=0
```

Restart PHP. `legacy-map init` already includes this option.

### trigger_trace timeout

Some endpoints are slow (external API calls, LLM, heavy processing).

```bash
# Increase the MCP server default timeout
legacy-map serve --http-timeout=60 ./xdebug-traces
```

Or in Claude Code, specify the timeout:
```
> "Trace POST /api/chat with a 120 second timeout"
```

### Empty call tree (only Kernel bootstrap)

The request failed before reaching application code (401, 404, 500 at framework level).

- **401**: endpoint requires authentication. Pass the JWT token in headers.
- **404**: wrong URL. Check with `bin/console debug:router`.
- **500**: PHP error. Check `var/log/dev.log`.

### Too much noise in the call tree

```bash
# Exclude specific namespaces
legacy-map serve --exclude-ns=Sentry\\,Jean85\\,ContainerXXX\\ ./xdebug-traces
```

### MCP not connecting in Claude Code

```bash
# Check the binary is accessible
which legacy-map

# Check MCP config
claude mcp list

# Restart Claude Code after MCP config changes
```

## Claude Code integration

For Claude to automatically use legacy-map instead of reading source code, add to your project's `CLAUDE.md`:

```markdown
## Tracing & Flow Analysis

This project has the legacy-map MCP server connected.
For any question about "what happens when...", "trace the flow of...",
or any execution flow analysis: use the legacy-map MCP tools
(trigger_trace, list_traces, get_last_trace, get_trace_by_uri)
INSTEAD OF reading source code statically.
```

## Prerequisites

- **Go 1.23+** (for `go install`)
- **PHP with XDebug 3.x** (on the target project)
- **Symfony** (or any PHP project — namespace filtering is configurable)

## Architecture

```
legacy-map (Go binary)
├── Parser      — streaming TSV (bufio.Scanner, handles 50-500 MB files)
├── Filter      — 3 layers (PHP internals, namespaces, vendor collapse)
├── CallTree    — tree reconstruction + merge + dedup
├── Watcher     — fsnotify, thread-safe ring buffer
└── MCP Server  — stdio, 4 tools for Claude Code / Cursor / Copilot
```

## License

MIT
