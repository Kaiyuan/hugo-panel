package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/ai"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

const (
	aiMaxTextRunes       = 60000
	aiMaxEditRunes       = 20000
	aiContinueContextLen = 12000
	aiMaxInstruction     = 2000
	aiTitleContextRunes  = 300
)

const aiWriterSystemPrompt = "You are a writing assistant embedded in a Markdown blog editor. " +
	"Follow the user's task exactly. Reply with the requested text only: no preamble, no explanations, " +
	"and no surrounding code fences. Preserve the language of the original text unless the task explicitly " +
	"asks for another language. Preserve Markdown syntax, headings, lists, links, images, shortcodes and code blocks."

const aiTitleSystemPrompt = "You translate blog post titles into short, natural English file names. " +
	"Reply with only the translated title as plain text. Do not use quotes, numbering or punctuation at the beginning or end."

type aiCompleteRequest struct {
	Task        string `json:"task"`
	Text        string `json:"text"`
	Instruction string `json:"instruction"`
	Title       string `json:"title"`
}

// handleAIComplete runs one writing-assistant action. Prompts live on the
// server so the editor only has to send a task and the article text.
func (s *Server) handleAIComplete(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req aiCompleteRequest
	if !decodeJSON(w, r, &req, 512*1024) {
		return
	}
	cfg, err := s.settings.AI(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load AI settings")
		return
	}
	if !cfg.Configured() {
		writeError(w, http.StatusBadRequest, "ai_not_configured", "Configure an AI provider in Settings → AI")
		return
	}
	task := strings.ToLower(strings.TrimSpace(req.Task))
	text := strings.TrimSpace(req.Text)
	instruction := strings.TrimSpace(req.Instruction)
	if len([]rune(instruction)) > aiMaxInstruction {
		writeError(w, http.StatusBadRequest, "invalid_request", "Instruction is too long")
		return
	}
	if len([]rune(text)) > aiMaxTextRunes {
		writeError(w, http.StatusBadRequest, "text_too_long", "The text is too long for the AI assistant")
		return
	}

	var system, user string
	maxTokens := 4096
	temperature := 0.5
	switch task {
	case "polish":
		if text == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Add some content before using AI")
			return
		}
		if len([]rune(text)) > aiMaxEditRunes {
			writeError(w, http.StatusBadRequest, "text_too_long", "Select a shorter passage to polish")
			return
		}
		system = aiWriterSystemPrompt
		user = "Polish the Markdown text below. Improve grammar, clarity and flow without changing its meaning, " +
			"facts or structure. Keep headings, lists, links, images, shortcodes and code blocks intact. " +
			"Return only the polished Markdown.\n\n---\n\n" + text
		maxTokens = 8192
		temperature = 0.3
	case "continue":
		if text == "" {
			text = strings.TrimSpace(req.Title)
		}
		if text == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Add a title or some content before using AI")
			return
		}
		system = aiWriterSystemPrompt
		user = "Continue the Markdown article below in the same language, tone and style. " +
			"Write a natural continuation of one to three paragraphs. Return only the new text, not the original.\n\n---\n\n" +
			tailRunes(text, aiContinueContextLen)
		maxTokens = 4096
		temperature = 0.7
	case "summary":
		if text == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Add some content before using AI")
			return
		}
		system = aiWriterSystemPrompt
		user = "Write a concise description of the Markdown article below for search results and social cards. " +
			"Use the article's language. Maximum 160 characters. Return only the description.\n\n---\n\n" + text
		maxTokens = 300
		temperature = 0.3
	case "custom":
		if instruction == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Describe what the AI should do")
			return
		}
		if text == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Add some content before using AI")
			return
		}
		if len([]rune(text)) > aiMaxEditRunes {
			writeError(w, http.StatusBadRequest, "text_too_long", "Select a shorter passage for this instruction")
			return
		}
		system = aiWriterSystemPrompt
		user = "Task: " + instruction + "\n\nMarkdown content:\n\n---\n\n" + text
		maxTokens = 8192
		temperature = 0.5
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "Unknown AI task")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	out, err := s.aiClient.Complete(ctx, cfg, []ai.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, maxTokens, temperature)
	if err != nil {
		s.logger.Warn("ai complete", "task", task, "model", cfg.Model, "error", err)
		writeError(w, http.StatusBadGateway, "ai_failed", err.Error())
		return
	}
	out = stripMarkdownFence(out)
	if out == "" {
		writeError(w, http.StatusBadGateway, "ai_empty", "The AI returned an empty result")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": out, "model": cfg.Model})
}

type aiTestRequest struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
}

// handleAITest checks the saved or unsaved provider settings. Provider
// failures are reported as ok=false so the settings page can show them inline.
func (s *Server) handleAITest(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req aiTestRequest
	if !decodeJSON(w, r, &req, 16*1024) {
		return
	}
	cfg, err := s.settings.AI(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load AI settings")
		return
	}
	if base := strings.TrimSpace(req.BaseURL); base != "" {
		cfg.BaseURL = base
	}
	if model := strings.TrimSpace(req.Model); model != "" {
		cfg.Model = model
	}
	if key := strings.TrimSpace(req.APIKey); key != "" {
		cfg.APIKey = key
	}
	cfg.Enabled = true
	cfg = cfg.Normalized()
	if cfg.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Enter a model name")
		return
	}
	if _, err := cfg.Endpoint(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	reply, err := s.aiClient.Complete(ctx, cfg, []ai.Message{
		{Role: "system", Content: "You are a connectivity test. Reply with only the word OK."},
		{Role: "user", Content: "ping"},
	}, 128, 0)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": cfg.Model, "reply": reply})
}

// stripMarkdownFence removes an outer ```markdown fence that some models add
// despite the prompt. A plain fence is kept because it may be real content.
func stripMarkdownFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") || !strings.HasSuffix(t, "```") || len(t) < 8 {
		return t
	}
	inner := t[3 : len(t)-3]
	nl := strings.IndexByte(inner, '\n')
	if nl <= 0 {
		return t
	}
	lang := strings.ToLower(strings.TrimSpace(inner[:nl]))
	if lang == "markdown" || lang == "md" {
		return strings.TrimSpace(inner[nl+1:])
	}
	return t
}

func tailRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[len(r)-limit:])
}
