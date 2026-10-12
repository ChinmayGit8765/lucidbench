package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

// Writing the config file. The UI saves a few settings for real: it sends
// the keys it changes, Lucidbench edits only those keys in config.yaml
// through the YAML node tree (so comments, ordering and keys it does not
// know stay as they were), checks that the result loads, backs the old file
// up next to it and replaces it atomically. The daemon reads its config at
// start, so a saved change applies after a restart; the reply says which
// keys are pending and which an environment variable overrides anyway.

// Kinds of value a writable key takes.
const (
	kindMode   = "mode"   // one of PowerModes
	kindInt    = "int"    // a whole number
	kindBool   = "bool"   // true or false
	kindString = "string" // any text
	kindSecret = "secret" // an env:NAME reference, never a value
)

// Writable lists the keys the UI may save, with the kind of value each takes.
var Writable = map[string]string{
	"power.cluster":              kindMode,
	"power.cluster_idle_minutes": kindInt,
	"power.runners":              kindMode,
	"power.runner_idle_minutes":  kindInt,
	"power.poll_seconds":         kindInt,
	"update_check":               kindBool,
	"vault.path":                 kindString,
	"integrations.linear.token":  kindSecret,
	"integrations.trello.key":    kindSecret,
	"integrations.trello.token":  kindSecret,
	"integrations.stripe.key":    kindSecret,
}

// KeepBackups is how many config.yaml.bak-* files are kept; older ones go.
const KeepBackups = 5

// ErrNoFile means there is no config file path to write to.
var ErrNoFile = errors.New("no config file path is known")

// DiffLine is one line of a change preview: Op is " " (kept), "-" or "+".
type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// Edit is what a set of changes would do to the config file.
type Edit struct {
	Path string `json:"path"`
	// Created is true when there was no file yet.
	Created bool `json:"created"`
	// Changed is false when the file already says all of it.
	Changed bool       `json:"changed"`
	Diff    []DiffLine `json:"diff"`
	// Overridden are keys an environment variable sets, so the file's value
	// does not count while it is set.
	Overridden []string `json:"overridden"`
	// Restart are keys whose saved value differs from what the running
	// daemon uses: they apply after Lucidbench restarts.
	Restart []string `json:"restart"`
	// Backup is the copy of the old file, once saved ("" when there was none).
	Backup string `json:"backup,omitempty"`

	data []byte
}

// writeMu serialises writes from this process.
var writeMu sync.Mutex

// PlanEdit works out what setting values would do to the file at path,
// without writing anything. running is the config the daemon uses now (for
// Restart); getenv the environment (for Overridden).
func PlanEdit(path string, set map[string]any, running *Config, getenv func(string) string) (*Edit, error) {
	if path == "" {
		return nil, ErrNoFile
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("nothing to change")
	}
	old, err := os.ReadFile(path)
	created := errors.Is(err, os.ErrNotExist)
	if err != nil && !created {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(old)) > 0 {
		if err := yaml.Unmarshal(old, &doc); err != nil {
			return nil, fmt.Errorf("config %s: invalid YAML, fix it by hand first: %w", path, err)
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config %s: the top level is not a mapping", path)
	}
	keys := make([]string, 0, len(set))
	vals := map[string]*yaml.Node{}
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, err := valueNode(k, set[k])
		if err != nil {
			return nil, err
		}
		vals[k] = v
	}
	// Edit the text in place when the file is plain block YAML, so blank
	// lines and formatting stay byte for byte; otherwise set the keys in
	// the node tree and encode it (comments and unknown keys still stay).
	data, ok := patchText(old, keys, vals)
	if !ok {
		for _, k := range keys {
			setKey(doc.Content[0], strings.Split(k, "."), vals[k])
		}
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(&doc); err != nil {
			return nil, err
		}
		_ = enc.Close()
		data = buf.Bytes()
	}

	// The result must load, or it is not written.
	next, err := loadBytes(path, data, getenv)
	if err != nil {
		return nil, err
	}
	e := &Edit{Path: path, Created: created, Changed: !bytes.Equal(old, data), Diff: lineDiff(string(old), string(data)), Overridden: []string{}, Restart: []string{}, data: data}
	for _, k := range keys {
		if src := next.Source(k); strings.HasPrefix(src, "env:") {
			e.Overridden = append(e.Overridden, k)
		}
		if running != nil && running.value(k) != next.value(k) {
			e.Restart = append(e.Restart, k)
		}
	}
	return e, nil
}

// SaveEdit writes the planned file: the old one is copied to
// <path>.bak-<time> first, then the new one replaces it atomically.
func SaveEdit(path string, set map[string]any, running *Config, getenv func(string) string) (*Edit, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	e, err := PlanEdit(path, set, running, getenv)
	if err != nil || !e.Changed {
		return e, err
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
		old, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		e.Backup = fmt.Sprintf("%s.bak-%s", path, time.Now().Format("20060102-150405.000"))
		if err := os.WriteFile(e.Backup, old, 0o600); err != nil {
			return nil, fmt.Errorf("back up %s: %w", path, err)
		}
		pruneBackups(path)
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, e.data, mode); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return e, nil
}

// pruneBackups keeps the newest KeepBackups backups of path.
func pruneBackups(path string) {
	m, _ := filepath.Glob(path + ".bak-*")
	sort.Strings(m) // the timestamp sorts in time order
	for len(m) > KeepBackups {
		_ = os.Remove(m[0])
		m = m[1:]
	}
}

// loadBytes is LoadFrom for a file's would-be content.
func loadBytes(path string, data []byte, getenv func(string) string) (*Config, error) {
	c := Default()
	c.File, c.FileFound = path, true
	if _, err := c.applyFile(path, data); err != nil {
		return nil, err
	}
	if err := c.applyEnv(getenv); err != nil {
		return nil, err
	}
	c.expandHome()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// valueNode turns a JSON value into the YAML scalar for a writable key.
func valueNode(key string, v any) (*yaml.Node, error) {
	kind, ok := Writable[key]
	if !ok {
		return nil, fmt.Errorf("%s cannot be changed from the UI; edit config.yaml", key)
	}
	bad := func(msg string) error { return fmt.Errorf("%s %s", key, msg) }
	switch kind {
	case kindInt:
		f, ok := v.(float64)
		if !ok || f != float64(int64(f)) {
			return nil, bad("must be a whole number")
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(int64(f))}, nil
	case kindBool:
		b, ok := v.(bool)
		if !ok {
			return nil, bad("must be true or false")
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(b)}, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, bad("must be text")
	}
	switch kind {
	case kindMode:
		if !validMode(s) {
			return nil, bad("must be one of " + strings.Join(PowerModes, ", "))
		}
	case kindSecret:
		// The error never echoes s: it may be a secret pasted by mistake.
		if _, err := ParseSecretRef(s); err != nil {
			return nil, bad("must name an environment variable as env:NAME; the secret itself is never saved")
		}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s, Style: yaml.DoubleQuotedStyle}, nil
}

// setKey sets the value at path under a mapping node, adding the mappings
// on the way when they are missing. A replaced value keeps the comments
// that were on the old one.
func setKey(m *yaml.Node, path []string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, cur := m.Content[i], m.Content[i+1]
		if k.Value != path[0] {
			continue
		}
		if len(path) == 1 {
			v.HeadComment, v.LineComment, v.FootComment = cur.HeadComment, cur.LineComment, cur.FootComment
			m.Content[i+1] = v
			return
		}
		if cur.Kind != yaml.MappingNode {
			// "power:" with nothing under it is null: it becomes a mapping.
			n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", LineComment: cur.LineComment}
			m.Content[i+1] = n
			cur = n
		}
		setKey(cur, path[1:], v)
		return
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: path[0]}
	if len(path) == 1 {
		m.Content = append(m.Content, key, v)
		return
	}
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	m.Content = append(m.Content, key, n)
	setKey(n, path[1:], v)
}

// patchText sets each key in the file's text: a value on one line is
// replaced where it stands (its comment kept), a missing key is added as a
// line at the end of its block, and a missing block at the end of the file.
// It reports false for what it does not handle (flow style, multi-line
// values, tabs), and when the result does not read back as intended.
func patchText(src []byte, keys []string, vals map[string]*yaml.Node) ([]byte, bool) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	crlf := strings.Contains(string(src), "\r\n")
	if strings.Contains(text, "\t") {
		return nil, false
	}
	for _, k := range keys {
		var ok bool
		if text, ok = patchKey(text, strings.Split(k, "."), vals[k]); !ok {
			return nil, false
		}
	}
	// Read back: every key holds its new value.
	var doc yaml.Node
	if yaml.Unmarshal([]byte(text), &doc) != nil || doc.Kind == 0 {
		return nil, false
	}
	for _, k := range keys {
		n := lookup(doc.Content[0], strings.Split(k, "."))
		if n == nil || n.Kind != yaml.ScalarNode || n.Value != vals[k].Value {
			return nil, false
		}
	}
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return []byte(text), true
}

// lookup finds the node at path under a mapping, nil when it is not there.
func lookup(m *yaml.Node, path []string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == path[0] {
			if len(path) == 1 {
				return m.Content[i+1]
			}
			return lookup(m.Content[i+1], path[1:])
		}
	}
	return nil
}

// scalarText is how a value is written on its line.
func scalarText(v *yaml.Node) string {
	if v.Style == yaml.DoubleQuotedStyle {
		out, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v.Value, Style: yaml.DoubleQuotedStyle})
		if err == nil {
			return strings.TrimRight(string(out), "\n")
		}
	}
	return v.Value
}

// lastLine is the last line a node and everything under it is on.
func lastLine(n *yaml.Node) int {
	l := n.Line
	for _, c := range n.Content {
		l = max(l, lastLine(c))
	}
	return l
}

func patchKey(text string, path []string, v *yaml.Node) (string, bool) {
	var doc yaml.Node
	if strings.TrimSpace(text) != "" {
		if yaml.Unmarshal([]byte(text), &doc) != nil {
			return "", false
		}
	}
	lines := strings.Split(text, "\n")
	// Walk down as far as the path exists.
	var m *yaml.Node
	if doc.Kind != 0 {
		m = doc.Content[0]
		if m.Kind != yaml.MappingNode || m.Style == yaml.FlowStyle {
			return "", false
		}
	}
	depth := 0
	for m != nil && depth < len(path) {
		var next *yaml.Node
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value != path[depth] {
				continue
			}
			val := m.Content[i+1]
			if depth == len(path)-1 {
				// Replace the value where it stands.
				if val.Kind != yaml.ScalarNode || val.Line != m.Content[i].Line || val.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
					return "", false
				}
				line := []rune(lines[val.Line-1])
				start := val.Column - 1
				end := scalarEnd(line, start, val)
				if end < 0 {
					return "", false
				}
				lines[val.Line-1] = string(line[:start]) + scalarText(v) + string(line[end:])
				return strings.Join(lines, "\n"), true
			}
			if val.Kind != yaml.MappingNode || val.Style == yaml.FlowStyle || len(val.Content) == 0 {
				return "", false
			}
			next = val
			break
		}
		if next == nil {
			break
		}
		m = next
		depth++
	}
	// path[depth:] is missing under m (nil: the file is empty).
	indent := 0
	at := len(lines)
	if m != nil && len(m.Content) > 0 {
		indent = m.Content[0].Column - 1
		at = lastLine(m)
		// After the block's last line, but before blank lines that follow it.
		for at > 0 && at <= len(lines) && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
	}
	var add []string
	for i, k := range path[depth:] {
		pad := strings.Repeat(" ", indent+2*i)
		if depth+i == len(path)-1 {
			add = append(add, pad+k+": "+scalarText(v))
		} else {
			add = append(add, pad+k+":")
		}
	}
	if m == nil || depth == 0 {
		// A new top-level block goes at the end of the file.
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > 0 && depth == 0 && len(path) > 1 {
			add = append([]string{""}, add...)
		}
		lines = append(lines, add...)
		return strings.Join(lines, "\n") + "\n", true
	}
	out := append(append(append([]string{}, lines[:at]...), add...), lines[at:]...)
	return strings.Join(out, "\n"), true
}

// scalarEnd is the index just past a one-line scalar that starts at start,
// -1 when it cannot be told.
func scalarEnd(line []rune, start int, n *yaml.Node) int {
	if start < 0 || start >= len(line) {
		return -1
	}
	switch n.Style {
	case yaml.DoubleQuotedStyle:
		for i := start + 1; i < len(line); i++ {
			if line[i] == '\\' {
				i++
				continue
			}
			if line[i] == '"' {
				return i + 1
			}
		}
		return -1
	case yaml.SingleQuotedStyle:
		for i := start + 1; i < len(line); i++ {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				return i + 1
			}
		}
		return -1
	}
	// Plain: up to a comment or the end of the line.
	end := len(line)
	for i := start; i < len(line); i++ {
		if line[i] == '#' && i > start && line[i-1] == ' ' {
			end = i
			break
		}
	}
	for end > start && line[end-1] == ' ' {
		end--
	}
	return end
}

// lineDiff is the lines of a and b as kept, removed and added, by the longest
// common subsequence. Runs of kept lines far from a change shrink to a
// "…" line so a preview shows the change with two lines around it.
func lineDiff(a, b string) []DiffLine {
	split := func(s string) []string {
		s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
		if s == "" {
			return nil
		}
		return strings.Split(s, "\n")
	}
	x, y := split(a), split(b)
	n, m := len(x), len(y)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var all []DiffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			all = append(all, DiffLine{" ", x[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			all = append(all, DiffLine{"-", x[i]})
			i++
		default:
			all = append(all, DiffLine{"+", y[j]})
			j++
		}
	}
	for ; i < n; i++ {
		all = append(all, DiffLine{"-", x[i]})
	}
	for ; j < m; j++ {
		all = append(all, DiffLine{"+", y[j]})
	}
	// Keep two lines of context around each change.
	const ctx = 2
	near := make([]bool, len(all))
	for k, l := range all {
		if l.Op == " " {
			continue
		}
		for d := max(0, k-ctx); d <= min(len(all)-1, k+ctx); d++ {
			near[d] = true
		}
	}
	out := []DiffLine{}
	gap := false
	for k, l := range all {
		if near[k] {
			out = append(out, l)
			gap = false
		} else if !gap {
			out = append(out, DiffLine{" ", "…"})
			gap = true
		}
	}
	return out
}
