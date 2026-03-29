# AIChatLog Server

Go REST API + MCP server for AIChatLog.

## Build & Run

```bash
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build -o aichatlog-server ./cmd/server
./aichatlog-server

# Development (dashboard hot-reload from disk, built-in test token)
make dev

# MCP mode
./aichatlog-server mcp --db /path/to/aichatlog.db
```

## Key Conventions

- Single dependency: `github.com/mattn/go-sqlite3`
- Module path: `github.com/aichatlog/aichatlog-server`
- Build requires: `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5"` for full-text search
- Env vars: `AICHATLOG_PORT`, `AICHATLOG_DB`, `AICHATLOG_DATA`, `AICHATLOG_TOKEN`, `AICHATLOG_DEV`
- Error handling: `(result, error)` tuples, wrap with `fmt.Errorf`
- SQL: dynamic query building with `?` placeholders
- Database: 6 tables (conversations, messages, tags, extractions, output_sync, process_queue) + FTS5
- Migration: numbered migrations via `schema_version` table (currently v2)
- Dedup key: `UNIQUE(source_type, device, session_id)`
- Dashboard: embedded via Go `embed` from `web/dashboard.html`
- MCP: `aichatlog-server mcp` subcommand, JSON-RPC 2.0 over stdin/stdout

## Output Adapters

All implement `Adapter` interface with `Name()`, `Push(path, content)`, `Test()`:
- **LocalAdapter** — Write .md files to local directory
- **FNSAdapter** — POST to Fast Note Sync API
- **WebhookAdapter** — POST JSON to any URL

## LLM Extraction

- Adapters: Anthropic (Claude Haiku/Sonnet), OpenAI-compatible
- Extracts: tech_solutions, concepts, work_log, prompts
- Results stored in `extractions` table

## MCP Server

- `aichatlog-server mcp --db path/to/db` — JSON-RPC over stdio
- Tools: `search_conversations`, `get_conversation`, `get_project_context`, `get_recent_work_log`
