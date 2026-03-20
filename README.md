# aichatlog-server

Universal AI conversation hub. Receives conversations from any source via the ConversationObject protocol, stores them in SQLite, and provides a REST API for querying.

## Quick Start

### Docker (recommended)

```bash
export AICHATLOG_TOKEN=your-secret-token
docker compose up -d
```

Server runs on `http://localhost:8080`.

### From source

```bash
go build -o aichatlog-server ./cmd/server
./aichatlog-server --port 8080 --token your-secret-token
```

## API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | Health check (no auth) |
| POST | `/api/conversations` | Receive ConversationObject |
| GET | `/api/conversations` | List (`?q=`, `?status=`, `?project=`, `?source=`, `?sort=`, `?order=`, `?limit=`, `?offset=`) |
| GET | `/api/conversations/:id` | Get single (`?full=true` for messages) |
| GET | `/api/stats` | Counts by status |

## ConversationObject Protocol

```json
{
  "version": 1,
  "source": "claude-code",
  "device": "macbook",
  "session_id": "unique-id",
  "title": "Conversation title",
  "date": "2026-03-19",
  "messages": [
    {"role": "user", "content": "...", "time_str": "14:30", "seq": 0},
    {"role": "assistant", "content": "...", "time_str": "14:31", "seq": 1}
  ]
}
```

The `source` field identifies the AI tool (`claude-code`, `chatgpt`, `gemini`, etc.). Any tool that POSTs this JSON can integrate.

## Configuration

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--port` | `AICHATLOG_PORT` | `8080` | Server port |
| `--db` | `AICHATLOG_DB` | `aichatlog.db` | SQLite database path |
| `--data` | `AICHATLOG_DATA` | `data` | Data directory |
| `--token` | `AICHATLOG_TOKEN` | *(none)* | Bearer token for auth |
