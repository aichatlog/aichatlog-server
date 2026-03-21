package llm

import (
	"fmt"
	"strings"

	"github.com/aichatlog/aichatlog/server/internal/storage"
)

// SystemPrompt returns the system prompt for knowledge extraction.
func SystemPrompt() string { return systemPromptText }

const systemPromptText = `You are a knowledge extraction assistant. You analyze AI coding assistant conversations and extract structured knowledge for a knowledge base.

Given a conversation between a user and an AI, extract the following (only include types that are relevant — not every conversation has all types):

1. tech_solutions: Technical problems solved with code
2. concepts: New concepts, technologies, or mental models explained
3. work_log: A one-line summary of what was accomplished
4. prompts: Particularly effective prompts worth reusing

Respond ONLY with valid JSON, no markdown fences, no preamble.`

const userPromptTemplate = `Analyze this conversation and extract knowledge.

Context:
- Source: %s
- Project: %s
- Date: %s
- Model: %s

Conversation:
%s

Respond with this JSON structure:
{
  "summary": "One sentence summary",
  "work_log_entry": "Concise log: what was done, key decisions",
  "tech_solutions": [
    {
      "title": "Short title",
      "problem": "What problem was being solved",
      "solution": "How it was solved",
      "code": "Key code snippet (if any)",
      "gotchas": ["Important caveats"],
      "tags": ["relevant", "tags"]
    }
  ],
  "concepts": [
    {
      "title": "Concept name",
      "explanation": "Clear explanation",
      "why_it_matters": "Practical relevance",
      "related": ["related concept"]
    }
  ],
  "prompts": [
    {
      "title": "Descriptive name",
      "prompt_text": "The actual prompt",
      "when_to_use": "Use case",
      "why_effective": "Why this works well"
    }
  ]
}

Rules:
- Only include sections genuinely present in the conversation
- tech_solutions: only if actual code/technical solution was provided
- concepts: only if a concept was explained in depth
- prompts: only if the user's prompt was notably effective (rare)
- work_log_entry: always include
- Write in the same language as the conversation
- Keep explanations concise but standalone-useful`

// BuildUserPrompt constructs the user prompt from conversation data.
func BuildUserPrompt(conv *storage.ConversationRow, messages []storage.MessageRow, maxWords int) string {
	// Format conversation content, truncated to maxWords
	var parts []string
	wordCount := 0
	for _, m := range messages {
		if m.IsContext {
			continue
		}
		role := strings.ToUpper(m.Role)
		line := fmt.Sprintf("[%s] %s", role, m.Content)

		words := len(strings.Fields(m.Content))
		if maxWords > 0 && wordCount+words > maxWords {
			remaining := maxWords - wordCount
			if remaining > 50 {
				fields := strings.Fields(m.Content)
				if len(fields) > remaining {
					line = fmt.Sprintf("[%s] %s ...(truncated)", role, strings.Join(fields[:remaining], " "))
				}
				parts = append(parts, line)
			}
			break
		}
		wordCount += words
		parts = append(parts, line)
	}

	content := strings.Join(parts, "\n\n")
	return fmt.Sprintf(userPromptTemplate, conv.SourceType, conv.Project, conv.StartedAt, conv.Model, content)
}
