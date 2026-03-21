// Package mcp implements a Model Context Protocol (MCP) server
// that exposes AIChatLog conversation search and retrieval as tools
// for AI assistants like Claude Code.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/aichatlog/aichatlog/server/internal/storage"
)

// Server implements the MCP protocol over stdin/stdout.
type Server struct {
	store  *storage.Store
	reader *bufio.Reader
	writer io.Writer
}

// NewServer creates a new MCP server backed by the given store.
func NewServer(store *storage.Store, in io.Reader, out io.Writer) *Server {
	return &Server{
		store:  store,
		reader: bufio.NewReader(in),
		writer: out,
	}
}

// JSON-RPC types

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP types

type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// Run starts the MCP server loop. Blocks until stdin is closed.
func (s *Server) Run() error {
	log.SetOutput(io.Discard) // MCP uses stdout, suppress log output

	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req jsonrpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "Parse error")
			continue
		}

		s.handleRequest(&req)
	}
}

func (s *Server) handleRequest(req *jsonrpcRequest) {
	switch req.Method {
	case "initialize":
		s.respond(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]string{
				"name":    "aichatlog",
				"version": "0.9.0",
			},
		})

	case "notifications/initialized":
		// No response needed for notifications

	case "tools/list":
		s.respond(req.ID, map[string]interface{}{
			"tools": s.listTools(),
		})

	case "tools/call":
		var params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		json.Unmarshal(req.Params, &params)
		result := s.callTool(params.Name, params.Arguments)
		s.respond(req.ID, result)

	default:
		s.sendError(req.ID, -32601, "Method not found: "+req.Method)
	}
}

func (s *Server) listTools() []toolDef {
	return []toolDef{
		{
			Name:        "search_conversations",
			Description: "Search past AI conversations by keyword. Returns matching conversations with title, project, date, model, and word count.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":   map[string]string{"type": "string", "description": "Search query (searches titles and projects)"},
					"project": map[string]string{"type": "string", "description": "Filter by project name"},
					"source":  map[string]string{"type": "string", "description": "Filter by source (e.g. claude-code, chatgpt)"},
					"limit":   map[string]interface{}{"type": "integer", "description": "Max results (default 20)", "default": 20},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_conversation",
			Description: "Get a full conversation with all messages by ID or session_id.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]string{"type": "string", "description": "Conversation ID or session_id"},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "get_project_context",
			Description: "Get recent conversations and extracted knowledge for a specific project. Useful for understanding project history and past decisions.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"project": map[string]string{"type": "string", "description": "Project name"},
					"limit":   map[string]interface{}{"type": "integer", "description": "Max conversations (default 10)", "default": 10},
				},
				"required": []string{"project"},
			},
		},
		{
			Name:        "get_recent_work_log",
			Description: "Get recent work log entries across all projects. Shows what was accomplished in recent AI conversations.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"days":  map[string]interface{}{"type": "integer", "description": "Number of days to look back (default 7)", "default": 7},
					"limit": map[string]interface{}{"type": "integer", "description": "Max entries (default 20)", "default": 20},
				},
			},
		},
	}
}

func (s *Server) callTool(name string, args map[string]interface{}) toolResult {
	switch name {
	case "search_conversations":
		return s.toolSearchConversations(args)
	case "get_conversation":
		return s.toolGetConversation(args)
	case "get_project_context":
		return s.toolGetProjectContext(args)
	case "get_recent_work_log":
		return s.toolGetRecentWorkLog(args)
	default:
		return toolResult{
			Content: []textContent{{Type: "text", Text: "Unknown tool: " + name}},
			IsError: true,
		}
	}
}

// ── Tool Implementations ──

func (s *Server) toolSearchConversations(args map[string]interface{}) toolResult {
	query, _ := args["query"].(string)
	project, _ := args["project"].(string)
	source, _ := args["source"].(string)
	limit := intArg(args, "limit", 20)

	rows, err := s.store.List(storage.QueryParams{
		Query:   query,
		Project: project,
		Source:  source,
		Limit:   limit,
		Sort:    "started_at",
		Order:   "desc",
	})
	if err != nil {
		return errorResult(err)
	}

	if len(rows) == 0 {
		return textResult("No conversations found matching: " + query)
	}

	var lines []string
	for _, r := range rows {
		line := fmt.Sprintf("- **%s** [%s] project=%s model=%s words=%d id=%s",
			r.Title, r.StartedAt[:minLen(r.StartedAt, 10)], r.Project, r.Model, r.WordCount, r.ID)
		lines = append(lines, line)
	}
	return textResult(fmt.Sprintf("Found %d conversations:\n\n%s", len(rows), strings.Join(lines, "\n")))
}

func (s *Server) toolGetConversation(args map[string]interface{}) toolResult {
	id, _ := args["id"].(string)
	if id == "" {
		return errorResult(fmt.Errorf("id is required"))
	}

	detail, err := s.store.GetDetail(id)
	if err != nil {
		return errorResult(err)
	}
	if detail == nil {
		return textResult("Conversation not found: " + id)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", detail.Title))
	sb.WriteString(fmt.Sprintf("> Date: %s | Project: %s | Source: %s | Model: %s\n",
		detail.StartedAt, detail.Project, detail.SourceType, detail.Model))
	sb.WriteString(fmt.Sprintf("> Messages: %d | Words: %d | Tokens: %d in / %d out\n\n---\n\n",
		detail.MessageCount, detail.WordCount, detail.TotalInputTokens, detail.TotalOutputTokens))

	for _, m := range detail.Messages {
		if m.IsContext {
			continue
		}
		role := strings.ToUpper(m.Role)
		ts := ""
		if m.Timestamp != "" {
			ts = " [" + m.Timestamp + "]"
		}
		sb.WriteString(fmt.Sprintf("### %s%s\n\n%s\n\n", role, ts, m.Content))
	}

	return textResult(sb.String())
}

func (s *Server) toolGetProjectContext(args map[string]interface{}) toolResult {
	project, _ := args["project"].(string)
	if project == "" {
		return errorResult(fmt.Errorf("project is required"))
	}
	limit := intArg(args, "limit", 10)

	// Recent conversations for this project
	rows, err := s.store.List(storage.QueryParams{
		Project: project,
		Limit:   limit,
		Sort:    "started_at",
		Order:   "desc",
	})
	if err != nil {
		return errorResult(err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Project: %s\n\n## Recent conversations (%d)\n\n", project, len(rows)))
	for _, r := range rows {
		sb.WriteString(fmt.Sprintf("- **%s** [%s] model=%s words=%d id=%s\n",
			r.Title, r.StartedAt[:minLen(r.StartedAt, 10)], r.Model, r.WordCount, r.ID))
	}

	// Extractions for this project's conversations
	sb.WriteString("\n## Extracted knowledge\n\n")
	for _, r := range rows {
		exts, _ := s.store.GetExtractions(r.ID)
		for _, e := range exts {
			sb.WriteString(fmt.Sprintf("- [%s] **%s**: %s\n", e.Type, e.Title, truncate(e.Content, 200)))
		}
	}

	return textResult(sb.String())
}

func (s *Server) toolGetRecentWorkLog(args map[string]interface{}) toolResult {
	limit := intArg(args, "limit", 20)

	exts, err := s.store.ListExtractions("work_log", limit, 0)
	if err != nil {
		return errorResult(err)
	}

	if len(exts) == 0 {
		return textResult("No work log entries found. LLM extraction may not be configured.")
	}

	var lines []string
	for _, e := range exts {
		lines = append(lines, fmt.Sprintf("- [%s] **%s**: %s", e.ExtractedAt[:minLen(e.ExtractedAt, 10)], e.Title, e.Content))
	}
	return textResult(fmt.Sprintf("Recent work log (%d entries):\n\n%s", len(exts), strings.Join(lines, "\n")))
}

// ── Helpers ──

func (s *Server) respond(id interface{}, result interface{}) {
	resp := jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: result}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", data)
}

func (s *Server) sendError(id interface{}, code int, msg string) {
	resp := jsonrpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", data)
}

func textResult(text string) toolResult {
	return toolResult{Content: []textContent{{Type: "text", Text: text}}}
}

func errorResult(err error) toolResult {
	return toolResult{Content: []textContent{{Type: "text", Text: "Error: " + err.Error()}}, IsError: true}
}

func intArg(args map[string]interface{}, key string, def int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return def
}

func minLen(s string, n int) int {
	if len(s) < n {
		return len(s)
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
