package processor

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/aichatlog/aichatlog/server/internal/storage"
)

// templateLabels holds per-language labels for markdown rendering.
var templateLabels = map[string]map[string]string{
	"en": {
		"date": "Date", "project": "Project", "source": "Source",
		"messages": "Messages", "words": "Words", "model": "Model",
		"tokens": "Tokens", "in": "in", "out": "out",
		"user": "USER", "assistant": "ASSISTANT",
	},
	"zh-CN": {
		"date": "日期", "project": "项目", "source": "来源",
		"messages": "消息", "words": "字数", "model": "模型",
		"tokens": "Token", "in": "输入", "out": "输出",
		"user": "用户", "assistant": "助手",
	},
	"zh-TW": {
		"date": "日期", "project": "專案", "source": "來源",
		"messages": "訊息", "words": "字數", "model": "模型",
		"tokens": "Token", "in": "輸入", "out": "輸出",
		"user": "用戶", "assistant": "助手",
	},
}

func getLabels(lang string) map[string]string {
	if l, ok := templateLabels[lang]; ok {
		return l
	}
	return templateLabels["en"]
}

// Default embedded templates (used when no custom template file exists).
const defaultConversationTemplate = `# {{.Title}}

> **{{.L.date}}:** {{.StartedAt}} | **{{.L.project}}:** {{.Project}} | **{{.L.source}}:** {{.SourceType}}
> **{{.L.messages}}:** {{.MessageCount}} | **{{.L.words}}:** {{.WordCount}}{{if .Model}} | **{{.L.model}}:** {{.Model}}{{end}}{{if or .TotalInputTokens .TotalOutputTokens}}
> **{{.L.tokens}}:** {{.TotalInputTokens}} {{.L.in}} / {{.TotalOutputTokens}} {{.L.out}}{{end}}

---

{{range .Messages}}{{if not .IsContext}}### {{roleLabel .Role}} {{if .Timestamp}}[{{.Timestamp}}]{{end}}

{{.Content}}

{{end}}{{end}}`

// TemplateDir is the custom template directory. If set, templates are loaded from files.
// Files in this directory override the embedded defaults.
var TemplateDir string

// loadTemplate loads a template from TemplateDir if available, otherwise uses the default.
func loadTemplate(name, defaultTmpl string) string {
	if TemplateDir != "" {
		path := filepath.Join(TemplateDir, name)
		if data, err := os.ReadFile(path); err == nil {
			log.Printf("template: loaded custom %s from %s", name, path)
			return string(data)
		}
	}
	return defaultTmpl
}

// RenderData is the data passed to the conversation template.
type RenderData struct {
	storage.ConversationRow
	Messages []storage.MessageRow
	L        map[string]string // i18n labels
}

// RenderMarkdown renders a conversation to markdown. lang is "en", "zh-CN", or "zh-TW".
func RenderMarkdown(conv *storage.ConversationRow, messages []storage.MessageRow, lang string) (string, error) {
	l := getLabels(lang)
	data := RenderData{
		ConversationRow: *conv,
		Messages:        messages,
		L:               l,
	}

	funcMap := template.FuncMap{
		"roleLabel": func(role string) string {
			if role == "user" {
				return l["user"]
			}
			return l["assistant"]
		},
	}

	tmplText := loadTemplate("conversation.md.tpl", defaultConversationTemplate)

	tmpl, err := template.New("conversation").Funcs(funcMap).Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
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

	month := "unknown"
	if t, err := time.Parse("2006-01-02", conv.StartedAt[:min(10, len(conv.StartedAt))]); err == nil {
		month = t.Format("2006-01")
	}

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
	return strings.TrimSpace(replacer.Replace(s))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
