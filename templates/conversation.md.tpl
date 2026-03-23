# {{.Title}}

> **{{.L.date}}:** {{.StartedAt}} | **{{.L.project}}:** {{.Project}} | **{{.L.source}}:** {{.SourceType}}
> **{{.L.messages}}:** {{.MessageCount}} | **{{.L.words}}:** {{.WordCount}}{{if .Model}} | **{{.L.model}}:** {{.Model}}{{end}}{{if or .TotalInputTokens .TotalOutputTokens}}
> **{{.L.tokens}}:** {{.TotalInputTokens}} {{.L.in}} / {{.TotalOutputTokens}} {{.L.out}}{{end}}

---

{{range .Messages}}{{if not .IsContext}}### {{roleLabel .Role}} {{if .Timestamp}}[{{.Timestamp}}]{{end}}

{{.Content}}

{{end}}{{end}}
