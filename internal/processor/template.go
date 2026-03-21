package processor

import (
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/aichatlog/aichatlog/server/internal/storage"
)

// defaultTemplate is the built-in markdown template for conversations.
const defaultTemplate = `# {{.Title}}

> **Date:** {{.StartedAt}} | **Project:** {{.Project}} | **Source:** {{.SourceType}}
> **Messages:** {{.MessageCount}} | **Words:** {{.WordCount}}{{if .Model}} | **Model:** {{.Model}}{{end}}

---

{{range .Messages}}{{if not .IsContext}}### {{upper .Role}} {{if .Timestamp}}[{{.Timestamp}}]{{end}}

{{.Content}}

{{end}}{{end}}`

var conversationTmpl *template.Template

func init() {
	funcMap := template.FuncMap{
		"upper": strings.ToUpper,
	}
	conversationTmpl = template.Must(template.New("conversation").Funcs(funcMap).Parse(defaultTemplate))
}

// RenderData is the data passed to the conversation template.
type RenderData struct {
	storage.ConversationRow
	Messages []storage.MessageRow
}

// RenderMarkdown renders a conversation to markdown using the built-in template.
func RenderMarkdown(conv *storage.ConversationRow, messages []storage.MessageRow) (string, error) {
	data := RenderData{
		ConversationRow: *conv,
		Messages:        messages,
	}
	var buf strings.Builder
	if err := conversationTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

// NotePath generates a relative file path for the conversation note.
// Format: aichatlog/YYYY-MM/Title.md
func NotePath(conv *storage.ConversationRow, syncDir string) string {
	if syncDir == "" {
		syncDir = "aichatlog"
	}

	// Parse date for folder grouping
	month := "unknown"
	if t, err := time.Parse("2006-01-02", conv.StartedAt[:min(10, len(conv.StartedAt))]); err == nil {
		month = t.Format("2006-01")
	}

	// Sanitize title for filename
	title := sanitizeFilename(conv.Title)
	if len(title) > 80 {
		title = title[:80]
	}
	if title == "" {
		title = conv.ID
	}

	return fmt.Sprintf("%s/%s/%s.md", syncDir, month, title)
}

func sanitizeFilename(s string) string {
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "", "?", "",
		"\"", "", "<", "", ">", "", "|", "", "\n", " ",
	)
	result := replacer.Replace(s)
	result = strings.TrimSpace(result)
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
