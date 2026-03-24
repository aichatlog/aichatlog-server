# Contributing to AIChatLog Server

Thanks for your interest in contributing! This guide covers the server component specifically.

## Getting Started

```bash
git clone https://github.com/aichatlog/aichatlog-server.git
cd aichatlog-server
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build -o aichatlog-server ./cmd/server
./aichatlog-server --port 8080
```

## Code Conventions

- Single external dependency: `github.com/mattn/go-sqlite3`
- Build requires: `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5"`
- Error handling: `(result, error)` tuples, wrap with `fmt.Errorf`
- Database migrations: numbered in `migrate()`, one function per version

## How to Contribute

### New Output Adapter

1. Create `internal/output/yourname.go`
2. Implement the `Adapter` interface: `Name()`, `Push(path, content)`, `Test()`
3. Add config type and factory case in `adapter.go`
4. Add to `config.go` ServerConfig

### New LLM Adapter

1. Create `internal/llm/yourname.go`
2. Implement `Adapter` interface: `Name()`, `Extract(system, user)`
3. Add config type and factory case in `adapter.go`

## Pull Request Process

1. Fork and create a feature branch
2. Make your changes
3. Verify: `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build ./cmd/server`
4. Submit PR with a clear description of what and why

## Related Repos

- [aichatlog-protocol](https://github.com/aichatlog/aichatlog-protocol) — ConversationObject spec
- [aichatlog-plugin-cc](https://github.com/aichatlog/aichatlog-plugin-cc) — Claude Code plugin
- [aichatlog-docs](https://github.com/aichatlog/aichatlog-docs) — Design documents

## License

AGPL-3.0 — see [LICENSE](LICENSE).
