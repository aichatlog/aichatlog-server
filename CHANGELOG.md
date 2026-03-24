# Changelog

All notable changes to AIChatLog Server will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/).

## [0.8.6] - 2026-03-24

### Added
- URL routing for server dashboard
- Inline shared UI components

## [0.8.5] - 2026-03-24

### Fixed
- Harden token security in plugin and server dashboards

## [0.8.4] - 2026-03-24

### Added
- Show/hide toggle for all password inputs

### Fixed
- Masked token/key placeholder in all password inputs
- Detailed error messages in LLM test/fetch UI

## [0.8.0] - 2026-03-23

### Added
- User auth system with username/password + API keys

## [0.7.0] - 2026-03-22

### Added
- Logo and favicon across all surfaces
- Multi-arch Docker build (amd64 + arm64)

### Changed
- Default port changed to 4180, container port remains 8080

## [0.6.0] - 2026-03-21

### Added
- Budget control system
- Notion output adapter
- Template system
- CI/CD pipeline
- Ollama config support
- FTS full-text search
- Timeline and Q&A turn views

### Fixed
- XML tag handling to prevent message swallowing
- Table header colors for light mode

## [0.5.0] - 2026-03-20

### Added
- Initial server extracted from monorepo
- REST API + MCP server
- SQLite storage with FTS5
- LLM knowledge extraction (Anthropic, OpenAI)
- Output adapters (Local, FNS, Webhook)
- Embedded web dashboard
