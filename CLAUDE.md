# AIChatLog Server

Go REST API + MCP server for AIChatLog.

## Build & Run

```bash
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build -o aichatlog-server ./cmd/server
./aichatlog-server

# Development (dashboard + shared UI hot-reload from disk, built-in test token)
make dev
# Dev mode serves /static/aichatlog-*.css|js from ../aichatlog-protocol/web/

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
- Migration: numbered migrations via `schema_version` table
- Dedup key: `UNIQUE(source_type, device, session_id)`
- Dashboard: embedded via Go `embed` from `web/dashboard.html`
- MCP: `aichatlog-server mcp` subcommand, JSON-RPC 2.0 over stdin/stdout

## API Endpoints

### Conversations
- `POST /api/conversations/sync` — v2 sync (check/delta/full)
- `POST /api/conversations` — v1 create
- `GET /api/conversations` — list with FTS search, filtering, pagination
- `GET /api/conversations/{id}?full=true` — get with messages

### File Storage
- `POST /api/files/upload` — multipart upload (source, device, session_id, file), returns server URL
- `GET /api/files/{convID}/{filename}` — serve stored file with immutable cache headers
- `GET /api/files` — list all files with conversation metadata
- `DELETE /api/files/{convID}/{filename}` — delete single file
- Files stored at `{AICHATLOG_DATA}/files/{convID}/{sha256}.{ext}` (content-addressed, deduped)
- `SoftDelete` auto-cleans associated files

### Other
- `GET /api/stats`, `GET /api/stats/summary` — analytics
- `GET /api/timeline` — chronological Q&A turns
- `GET /api/extractions` — LLM-extracted knowledge
- Auth: API keys, session cookies, legacy bearer token

## Dashboard

Embedded SPA with 5 tabs: Conversations, Knowledge, Timeline, Files, Settings.

- **Files tab**: Grid view of all stored files, image thumbnails, file stats, delete support
- Shared UI components loaded from `aichatlog-protocol/web/` (CDN in prod, `/static/` in dev)
- Message rendering: markdown-it + DOMPurify + highlight.js via `<aichatlog-message>` web component

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
