# aichatlog-server

[English](README.md) | 简体中文

通用 AI 对话中心。通过 ConversationObject 协议从任何来源接收对话，存储到 SQLite，并提供 REST API 用于查询和管理。

## 快速开始

### Docker（推荐）

```bash
export AICHATLOG_TOKEN=your-secret-token
docker compose up -d
```

服务器运行在 `http://localhost:8080`，打开即可看到管理面板。

### 从源码编译

```bash
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build -o aichatlog-server ./cmd/server
./aichatlog-server --port 8080 --token your-secret-token
```

### MCP 模式

供 Claude Code 等 AI 助手搜索和查询对话历史：

```bash
./aichatlog-server mcp --db /path/to/aichatlog.db
```

## API

| 方法 | 端点 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查（无需认证） |
| POST | `/api/conversations` | 接收 ConversationObject |
| POST | `/api/conversations/sync` | v2 条件同步（check/delta/full） |
| POST | `/api/conversations/batch` | 批量接收 |
| GET | `/api/conversations` | 列表查询（`?q=`、`?status=`、`?project=`、`?source=`、`?sort=`、`?order=`、`?limit=`、`?offset=`） |
| GET | `/api/conversations/:id` | 获取单条（`?full=true` 包含消息） |
| GET | `/api/conversations/:id/messages` | 获取消息列表 |
| DELETE | `/api/conversations/:id` | 软删除（不会被重新同步覆盖） |
| PATCH | `/api/conversations/:id` | 更新字段（title、project、status） |
| POST | `/api/conversations/:id/extract` | 手动触发 LLM 知识提取 |
| POST | `/api/conversations/:id/reprocess` | 重新排队处理 |
| GET | `/api/stats` | 按状态统计 |
| GET | `/api/stats/summary` | 扩展统计（Token、字数、提取数、项目数） |
| GET | `/api/projects` | 项目列表（含对话数和活动时间） |
| GET | `/api/extractions` | 知识提取列表（`?type=`、`?limit=`、`?offset=`） |
| GET | `/api/conversations/:id/extractions` | 单条对话的提取结果 |
| GET | `/api/config` | 读取配置 |
| POST | `/api/config` | 更新配置 |

## ConversationObject 协议

```json
{
  "version": 1,
  "source": "claude-code",
  "device": "macbook",
  "session_id": "unique-id",
  "title": "对话标题",
  "date": "2026-03-22",
  "model": "claude-opus-4-6",
  "total_input_tokens": 5000,
  "total_output_tokens": 1200,
  "metadata": {"git_branch": "main", "entrypoint": "claude-vscode"},
  "messages": [
    {"role": "user", "content": "...", "timestamp": "2026-03-22T10:00:00.000Z", "seq": 0},
    {"role": "assistant", "content": "...", "timestamp": "2026-03-22T10:00:15.000Z", "seq": 1, "model": "claude-opus-4-6", "input_tokens": 5000, "output_tokens": 1200}
  ]
}
```

`source` 字段标识 AI 工具（`claude-code`、`chatgpt`、`gemini` 等）。任何 POST 此 JSON 的工具都能接入。

## 配置

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `--port` | `AICHATLOG_PORT` | `8080` | 服务器端口 |
| `--db` | `AICHATLOG_DB` | `aichatlog.db` | SQLite 数据库路径 |
| `--data` | `AICHATLOG_DATA` | `data` | 数据目录 |
| `--token` | `AICHATLOG_TOKEN` | *（无）* | Bearer 认证 Token |
| `--config` | — | `<data>/config.json` | 配置文件路径 |

### config.json 示例

```json
{
  "lang": "zh-CN",
  "output": {
    "adapter": "local",
    "local": {"path": "/path/to/output"}
  },
  "processor": {
    "enabled": true,
    "interval_seconds": 30,
    "batch_size": 20,
    "sync_dir": "aichatlog"
  },
  "llm": {
    "adapter": "anthropic",
    "anthropic": {"api_key": "sk-...", "model": "claude-haiku-4-5-20251001"},
    "min_words": 100
  }
}
```

## 管理面板

访问 `http://localhost:8080` 查看内置管理面板，包含四个标签页：

- **对话** — 浏览、搜索、过滤、排序对话列表，点击查看详情和执行操作（删除、重新处理、提取知识）
- **知识库** — 按类型浏览 LLM 提取的知识（技术方案、概念、提示词、工作日志）
- **时间线** — 按时间倒序查看工作日志
- **设置** — 查看统计概览和配置信息
