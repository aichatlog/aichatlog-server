package processor

import (
	"testing"

	"github.com/aichatlog/aichatlog-server/internal/llm"
)

func TestParseExtraction(t *testing.T) {
	cases := []string{
		`{"summary":"s","work_log_entry":"w"}`,
		"```json\n{\"summary\":\"s\",\"work_log_entry\":\"w\"}\n```",
		"好的，结果如下：\n{\"summary\":\"s\",\"work_log_entry\":\"w\"}\n以上。",
	}
	for _, raw := range cases {
		var r llm.ExtractionResult
		if err := parseExtraction(raw, &r); err != nil || r.Summary != "s" || r.WorkLogEntry != "w" {
			t.Errorf("parseExtraction(%q) = %+v, %v", raw, r, err)
		}
	}
	var r llm.ExtractionResult
	if err := parseExtraction(`{"summary":"s", "work_log_entry": "w"`, &r); err == nil {
		t.Errorf("truncated JSON should fail")
	}
}
