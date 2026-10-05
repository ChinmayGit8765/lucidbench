package agentexec

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxRaw is the largest CLI line kept in Event.Raw.
const maxRaw = 64 << 10

// maxBody is the longest Event.Body; the full line stays in Raw when small.
const maxBody = 8 << 10

// streamParser turns a CLI's JSON lines into Events.
type streamParser interface {
	line([]byte)
	finish()
	// result fills usage and returns the answer, whether the run reported an
	// error, and the error messages it printed.
	result(u *Usage) (text string, isErr bool, errs []string)
}

func newStreamParser(provider string, emit func(Event)) streamParser {
	b := base{emit: emit, names: map[string]string{}}
	switch provider {
	case "claude":
		return &claudeParser{base: b}
	case "codex":
		return &codexParser{base: b}
	}
	return &grokParser{base: b}
}

type base struct {
	emit  func(Event)
	names map[string]string // tool call id -> tool name
}

func (b *base) event(kind, title, body string, raw []byte) {
	e := Event{Time: time.Now(), Kind: kind, Title: title, Body: clip(body, maxBody)}
	if len(raw) > 0 && len(raw) <= maxRaw {
		e.Raw = append(json.RawMessage(nil), raw...)
	}
	b.emit(e)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Do not cut a UTF-8 sequence in half.
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}

// lineWriter splits what a CLI writes into lines, however large.
type lineWriter struct {
	buf []byte
	fn  func([]byte)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	start := 0
	for {
		i := bytes.IndexByte(w.buf[start:], '\n')
		if i < 0 {
			break
		}
		if line := bytes.TrimSpace(w.buf[start : start+i]); len(line) > 0 {
			w.fn(line)
		}
		start += i + 1
	}
	w.buf = append(w.buf[:0], w.buf[start:]...)
	return len(p), nil
}

// flush handles a last line that had no newline.
func (w *lineWriter) flush() {
	if line := bytes.TrimSpace(w.buf); len(line) > 0 {
		w.fn(line)
	}
	w.buf = nil
}

// diffBody renders an edit as a minimal unified-style body.
func diffBody(oldText, newText string) string {
	var sb strings.Builder
	for _, l := range splitLines(oldText) {
		sb.WriteString("- " + l + "\n")
	}
	for _, l := range splitLines(newText) {
		sb.WriteString("+ " + l + "\n")
	}
	return sb.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// ---- claude ----

// claudeParser reads `claude -p --output-format stream-json --verbose`.
type claudeParser struct {
	base
	last  []byte
	text  string
	isErr bool
	// partial is the usage each assistant message reported so far, by message
	// id, for a run that ends before its final "result" line.
	partial map[string]partialUsage
}

type partialUsage struct{ in, out, cacheRead, cacheWrite int64 }

func (p *claudeParser) line(b []byte) {
	var m struct {
		Type    string `json:"type"`
		Message struct {
			ID      string            `json:"id"`
			Content []json.RawMessage `json:"content"`
			Usage   *struct {
				In         int64 `json:"input_tokens"`
				Out        int64 `json:"output_tokens"`
				CacheRead  int64 `json:"cache_read_input_tokens"`
				CacheWrite int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(b, &m) != nil {
		return
	}
	switch m.Type {
	case "assistant":
		if u := m.Message.Usage; u != nil && m.Message.ID != "" {
			if p.partial == nil {
				p.partial = map[string]partialUsage{}
			}
			p.partial[m.Message.ID] = partialUsage{u.In, u.Out, u.CacheRead, u.CacheWrite}
		}
		for _, raw := range m.Message.Content {
			var blk struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if json.Unmarshal(raw, &blk) != nil {
				continue
			}
			switch blk.Type {
			case "text":
				if strings.TrimSpace(blk.Text) != "" {
					p.event(KindText, "", blk.Text, raw)
				}
			case "tool_use":
				p.names[blk.ID] = blk.Name
				p.event(KindTool, blk.Name, string(blk.Input), raw)
				p.edit(blk.Name, blk.Input)
			}
		}
	case "user":
		for _, raw := range m.Message.Content {
			var blk struct {
				Type    string          `json:"type"`
				ID      string          `json:"tool_use_id"`
				Content json.RawMessage `json:"content"`
				IsError bool            `json:"is_error"`
			}
			if json.Unmarshal(raw, &blk) != nil || blk.Type != "tool_result" {
				continue
			}
			title := p.names[blk.ID]
			if blk.IsError {
				title += " (error)"
			}
			p.event(KindToolResult, strings.TrimSpace(title), contentText(blk.Content), raw)
		}
	case "result":
		p.last = append(p.last[:0], b...)
	}
}

// edit emits a diff event for the tools that change files.
func (p *claudeParser) edit(name string, input json.RawMessage) {
	var in struct {
		Path    string `json:"file_path"`
		Old     string `json:"old_string"`
		New     string `json:"new_string"`
		Content string `json:"content"`
		Edits   []struct {
			Old string `json:"old_string"`
			New string `json:"new_string"`
		} `json:"edits"`
	}
	if json.Unmarshal(input, &in) != nil {
		return
	}
	switch name {
	case "Edit":
		p.event(KindDiff, in.Path, diffBody(in.Old, in.New), nil)
	case "Write":
		p.event(KindDiff, in.Path, diffBody("", in.Content), nil)
	case "MultiEdit":
		var sb strings.Builder
		for _, e := range in.Edits {
			sb.WriteString(diffBody(e.Old, e.New))
		}
		p.event(KindDiff, in.Path, sb.String(), nil)
	}
}

// contentText flattens a tool_result content, a string or a list of blocks.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func (p *claudeParser) finish() {}

func (p *claudeParser) result(u *Usage) (string, bool, []string) {
	if len(p.last) == 0 {
		// No final line: the run was cut short. Report what was streamed.
		for _, pu := range p.partial {
			u.InputTokens += pu.in
			u.OutputTokens += pu.out
			u.CacheRead += pu.cacheRead
			u.CacheWrite += pu.cacheWrite
		}
		return "", false, nil
	}
	text, isErr := parseClaude(p.last, u)
	return text, isErr, nil
}

// parseClaude reads `claude -p --output-format json`, or the final "result"
// line of the stream: the answer is in "result", with cost and token counts
// alongside.
func parseClaude(out []byte, u *Usage) (string, bool) {
	var env struct {
		IsError bool    `json:"is_error"`
		Result  string  `json:"result"`
		Cost    float64 `json:"total_cost_usd"`
		Usage   struct {
			In         int64 `json:"input_tokens"`
			Out        int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &env); err != nil {
		return string(out), false
	}
	u.InputTokens, u.OutputTokens = env.Usage.In, env.Usage.Out
	u.CacheRead, u.CacheWrite, u.CostUSD = env.Usage.CacheRead, env.Usage.CacheWrite, env.Cost
	if m := modelNames(env.ModelUsage); m != "" {
		u.Model = m
	}
	return env.Result, env.IsError
}

func modelNames(m map[string]json.RawMessage) string {
	models := make([]string, 0, len(m))
	for k := range m {
		models = append(models, k)
	}
	sort.Strings(models)
	return strings.Join(models, ", ")
}

// ---- codex ----

// codexParser reads `codex exec --json`.
type codexParser struct {
	base
	text  string
	errs  []string
	usage Usage
	turns int
}

func (p *codexParser) line(b []byte) {
	var m struct {
		Type    string          `json:"type"`
		Item    json.RawMessage `json:"item"`
		Message string          `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage struct {
			In         int64 `json:"input_tokens"`
			Cached     int64 `json:"cached_input_tokens"`
			CacheWrite int64 `json:"cache_write_input_tokens"`
			Out        int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(b, &m) != nil {
		return
	}
	switch m.Type {
	case "item.started", "item.completed":
		p.item(m.Type == "item.completed", m.Item, b)
	case "turn.completed":
		p.turns++
		p.usage.InputTokens += m.Usage.In
		p.usage.OutputTokens += m.Usage.Out
		p.usage.CacheRead += m.Usage.Cached
		p.usage.CacheWrite += m.Usage.CacheWrite
	case "turn.failed":
		p.errs = append(p.errs, m.Error.Message)
		p.event(KindError, "turn failed", m.Error.Message, b)
	case "error":
		p.errs = append(p.errs, m.Message)
		p.event(KindError, "", m.Message, b)
	}
}

func (p *codexParser) item(done bool, raw json.RawMessage, line []byte) {
	var it struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Text     string `json:"text"`
		Message  string `json:"message"`
		Command  string `json:"command"`
		Output   string `json:"aggregated_output"`
		ExitCode *int   `json:"exit_code"`
		Server   string `json:"server"`
		Tool     string `json:"tool"`
		Query    string `json:"query"`
		Changes  []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"changes"`
	}
	if json.Unmarshal(raw, &it) != nil {
		return
	}
	switch it.Type {
	case "agent_message":
		if done {
			p.text = it.Text
			p.event(KindText, "", it.Text, line)
		}
	case "command_execution":
		if !done {
			p.names[it.ID] = it.Command
			p.event(KindTool, "command", it.Command, line)
			return
		}
		if _, seen := p.names[it.ID]; !seen {
			p.event(KindTool, "command", it.Command, nil)
		}
		title := it.Command
		if it.ExitCode != nil && *it.ExitCode != 0 {
			title += " (exit " + strconv.Itoa(*it.ExitCode) + ")"
		}
		p.event(KindToolResult, title, it.Output, line)
	case "mcp_tool_call", "web_search":
		if !done {
			title := it.Server + "." + it.Tool
			if it.Type == "web_search" {
				title = "web_search"
			}
			p.event(KindTool, title, it.Query, line)
		}
	case "file_change":
		if !done {
			return
		}
		var sb strings.Builder
		title := ""
		for _, c := range it.Changes {
			sb.WriteString(c.Kind + " " + c.Path + "\n")
			if title == "" {
				title = c.Path
			} else {
				title = strconv.Itoa(len(it.Changes)) + " files"
			}
		}
		p.event(KindDiff, title, sb.String(), line)
	case "error":
		// A warning from the CLI itself (a clamped hook timeout, a skills
		// budget); it does not fail the run.
		if done {
			p.event(KindError, "warning", it.Message, line)
		}
	}
}

func (p *codexParser) finish() {}

func (p *codexParser) result(u *Usage) (string, bool, []string) {
	u.InputTokens, u.OutputTokens = p.usage.InputTokens, p.usage.OutputTokens
	u.CacheRead, u.CacheWrite = p.usage.CacheRead, p.usage.CacheWrite
	return p.text, len(p.errs) > 0 && p.turns == 0, p.errs
}

// parseCodexUsage reads the usage out of `codex exec --json` output whose
// answer was written to a file.
func parseCodexUsage(out []byte, u *Usage) {
	p := &codexParser{base: base{emit: func(Event) {}, names: map[string]string{}}}
	lw := &lineWriter{fn: p.line}
	_, _ = lw.Write(out)
	lw.flush()
	u.InputTokens, u.OutputTokens = p.usage.InputTokens, p.usage.OutputTokens
	u.CacheRead, u.CacheWrite = p.usage.CacheRead, p.usage.CacheWrite
}

// ---- grok ----

// grokParser reads `grok --output-format streaming-json`: one ACP session
// update per line. Text arrives in small deltas.
type grokParser struct {
	base
	pending strings.Builder // text deltas not yet emitted
	text    string          // the text since the last tool call
	prev    string
	end     json.RawMessage
	errs    []string
}

func (p *grokParser) flush() {
	if s := p.pending.String(); strings.TrimSpace(s) != "" {
		p.event(KindText, "", s, nil)
		p.prev, p.text = p.text, s
	}
	p.pending.Reset()
}

func (p *grokParser) line(b []byte) {
	var m struct {
		Type       string          `json:"type"`
		Data       json.RawMessage `json:"data"`
		ID         string          `json:"toolCallId"`
		ToolName   string          `json:"toolName"`
		Title      string          `json:"title"`
		Status     string          `json:"status"`
		RawInput   json.RawMessage `json:"rawInput"`
		RawOutput  json.RawMessage `json:"rawOutput"`
		StopReason string          `json:"stopReason"`
		Message    string          `json:"message"`
		Content    []struct {
			Type    string `json:"type"`
			Path    string `json:"path"`
			OldText string `json:"oldText"`
			NewText string `json:"newText"`
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"content"`
	}
	if json.Unmarshal(b, &m) != nil {
		return
	}
	switch m.Type {
	case "text":
		var s string
		if json.Unmarshal(m.Data, &s) == nil {
			p.pending.WriteString(s)
		}
	case "tool_call":
		p.flush()
		name := m.ToolName
		if name == "" {
			name = m.Title
		}
		p.names[m.ID] = name
		p.event(KindTool, name, string(m.RawInput), b)
	case "tool_call_update":
		if m.Status != "completed" && m.Status != "failed" {
			return
		}
		p.flush()
		var texts []string
		for _, c := range m.Content {
			switch c.Type {
			case "diff":
				p.event(KindDiff, c.Path, diffBody(c.OldText, c.NewText), nil)
			case "content":
				if c.Content.Text != "" {
					texts = append(texts, c.Content.Text)
				}
			}
		}
		title := p.names[m.ID]
		if m.Status == "failed" {
			title += " (failed)"
		}
		body := strings.Join(texts, "\n")
		if body == "" {
			body = grokOutput(m.RawOutput)
		}
		p.event(KindToolResult, title, body, b)
	case "end":
		p.flush()
		p.end = append(p.end[:0], b...)
	case "error":
		msg := m.Message
		if msg == "" {
			_ = json.Unmarshal(m.Data, &msg)
		}
		p.errs = append(p.errs, msg)
		p.event(KindError, "", msg, b)
	}
}

// grokOutput picks the readable part of a tool's rawOutput.
func grokOutput(raw json.RawMessage) string {
	var v map[string]map[string]any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	for _, inner := range v {
		for _, k := range []string{"tool_output_for_prompt_concise", "raw_output", "content_concise", "content"} {
			if s, ok := inner[k].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func (p *grokParser) finish() { p.flush() }

func (p *grokParser) result(u *Usage) (string, bool, []string) {
	text := p.text
	if strings.TrimSpace(text) == "" {
		text = p.prev
	}
	if len(p.end) > 0 {
		var e struct {
			Usage struct {
				In         int64 `json:"input_tokens"`
				Out        int64 `json:"output_tokens"`
				CacheRead  int64 `json:"cache_read_input_tokens"`
				CacheWrite int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
			Cost       float64                    `json:"total_cost_usd"`
			ModelUsage map[string]json.RawMessage `json:"modelUsage"`
		}
		if json.Unmarshal(p.end, &e) == nil {
			u.InputTokens, u.OutputTokens = e.Usage.In, e.Usage.Out
			u.CacheRead, u.CacheWrite, u.CostUSD = e.Usage.CacheRead, e.Usage.CacheWrite, e.Cost
			if m := modelNames(e.ModelUsage); m != "" {
				u.Model = m
			}
		}
	}
	return text, len(p.errs) > 0 && len(p.end) == 0, p.errs
}

// parseGeneric reads a JSON envelope whose answer sits in one of the usual
// fields, or returns the output as is.
func parseGeneric(out []byte, u *Usage) string {
	var env map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimSpace(out), &env) != nil {
		return string(out)
	}
	for _, k := range []string{"result", "text", "content", "response", "output", "message"} {
		var s string
		if raw, ok := env[k]; ok && json.Unmarshal(raw, &s) == nil && s != "" {
			var usage struct {
				In         int64 `json:"input_tokens"`
				Out        int64 `json:"output_tokens"`
				CacheRead  int64 `json:"cache_read_input_tokens"`
				CacheWrite int64 `json:"cache_creation_input_tokens"`
			}
			if raw, ok := env["usage"]; ok {
				_ = json.Unmarshal(raw, &usage)
				u.InputTokens, u.OutputTokens = usage.In, usage.Out
				u.CacheRead, u.CacheWrite = usage.CacheRead, usage.CacheWrite
			}
			if raw, ok := env["total_cost_usd"]; ok {
				_ = json.Unmarshal(raw, &u.CostUSD)
			}
			if raw, ok := env["modelUsage"]; ok {
				var mu map[string]json.RawMessage
				if json.Unmarshal(raw, &mu) == nil {
					if m := modelNames(mu); m != "" {
						u.Model = m
					}
				}
			}
			return s
		}
	}
	return string(out)
}
