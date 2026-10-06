package databases

import (
	"context"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of the query box.
const (
	RowLimit     = 500
	QueryTimeout = 10 * time.Second
	// ConnectTimeout bounds a connect and ping.
	ConnectTimeout = 5 * time.Second
	maxCellBytes   = 4096
)

// Info is what a health check learns.
type Info struct {
	Version string `json:"version"`
	// SizeBytes is -1 when the engine does not say.
	SizeBytes int64 `json:"size_bytes"`
}

// Column is one column of a table (or one field of a sampled document).
type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Nullable bool   `json:"nullable,omitempty"`
}

// Table is a table, view, collection or key, with a row estimate.
type Table struct {
	Schema string `json:"schema,omitempty"`
	Name   string `json:"name"`
	// Kind is table, view, collection, or for Redis the key's type.
	Kind string `json:"kind"`
	// Rows is an estimate; nil when the engine has none.
	Rows    *int64   `json:"rows,omitempty"`
	Columns []Column `json:"columns"`
}

// Result is the answer to a query.
type Result struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	// Nulls marks cells that are SQL NULL, so they differ from the text "NULL".
	Nulls     [][]bool `json:"nulls,omitempty"`
	Truncated bool     `json:"truncated"`
	RowLimit  int      `json:"row_limit"`
	ElapsedMS int64    `json:"elapsed_ms"`
	Note      string   `json:"note,omitempty"`
}

// Conn is one open connection to a database.
type Conn interface {
	Ping(ctx context.Context) error
	Info(ctx context.Context) (Info, error)
	Schema(ctx context.Context) ([]Table, error)
	// Query runs a read-only query. It returns ErrReadOnly for anything that
	// could change data; the engine enforces the same where it can.
	Query(ctx context.Context, q string) (*Result, error)
	Close()
}

// Opener connects to a profile with its resolved password.
type Opener func(ctx context.Context, p Profile, password string) (Conn, error)

// ErrReadOnly is returned for a query that is not read-only.
var ErrReadOnly = errors.New("read-only: this connection runs reads only; writes are not supported yet")

// ErrEmptyQuery is returned for a blank query.
var ErrEmptyQuery = errors.New("write a query first")

// readStarts are the statements the SQL query box accepts. TABLE and VALUES
// are Postgres and MySQL reads, DESCRIBE and SHOW are MySQL and Postgres
// reads. A WITH may still carry a write (a data-modifying CTE), which the
// read-only transaction refuses.
var readStarts = map[string]bool{
	"select": true, "with": true, "show": true, "explain": true,
	"values": true, "table": true, "describe": true, "desc": true,
}

var intoFileRE = regexp.MustCompile(`(?is)\binto\s+(outfile|dumpfile)\b`)

// stripSQL removes comments and the contents of quoted strings and
// identifiers, so keywords and semicolons are only seen where they act.
func stripSQL(q string, backslash bool) string {
	var b strings.Builder
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == '-' && strings.HasPrefix(q[i:], "--"):
			for i < len(q) && q[i] != '\n' {
				i++
			}
		case c == '/' && strings.HasPrefix(q[i:], "/*"):
			end := strings.Index(q[i+2:], "*/")
			if end < 0 {
				i = len(q)
			} else {
				i += end + 4
			}
			b.WriteByte(' ')
		case c == '\'' || c == '"' || c == '`':
			i++
			for i < len(q) {
				if backslash && q[i] == '\\' && c != '`' {
					i += 2
					continue
				}
				if q[i] == c {
					if i+1 < len(q) && q[i+1] == c {
						i += 2
						continue
					}
					break
				}
				i++
			}
			i++
			b.WriteString("''")
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// CheckSQL refuses a statement that is not a single read. It is the first
// gate; the read-only transaction is the one that holds if this one is fooled.
// Backslash is an escape in MySQL strings and not in Postgres ones, so the
// query must pass read both ways.
func CheckSQL(q string) error {
	if err := checkSQL(q, true); err != nil {
		return err
	}
	return checkSQL(q, false)
}

func checkSQL(q string, backslash bool) error {
	s := strings.TrimSpace(stripSQL(q, backslash))
	if s == "" {
		return ErrEmptyQuery
	}
	s = strings.TrimSpace(strings.TrimRight(s, "; \t\r\n"))
	if strings.Contains(s, ";") {
		return fmt.Errorf("%w (one statement at a time)", ErrReadOnly)
	}
	first := strings.ToLower(strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '(' })[0])
	if !readStarts[first] {
		return fmt.Errorf("%w (%s is not a read)", ErrReadOnly, strings.ToUpper(first))
	}
	if intoFileRE.MatchString(s) {
		return fmt.Errorf("%w (INTO OUTFILE writes a file)", ErrReadOnly)
	}
	return nil
}

// cell turns a driver value into display text and says whether it was NULL.
func cell(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case string:
		return clip(x), false
	case []byte:
		if utf8.Valid(x) {
			return clip(string(x)), false
		}
		return clip("\\x" + hex.EncodeToString(x)), false
	case time.Time:
		return x.Format(time.RFC3339Nano), false
	case [16]byte:
		h := hex.EncodeToString(x[:])
		return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], false
	case bool:
		return fmt.Sprint(x), false
	case driver.Valuer:
		if dv, err := x.Value(); err == nil {
			return cell(dv)
		}
	case fmt.Stringer:
		return clip(x.String()), false
	}
	switch x := v.(type) {
	case map[string]any, []any:
		if b, err := json.Marshal(x); err == nil {
			return clip(string(b)), false
		}
	}
	return clip(fmt.Sprint(v)), false
}

func clip(s string) string {
	if len(s) <= maxCellBytes {
		return s
	}
	cut := maxCellBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// rowSink collects up to RowLimit rows and notes that more existed.
type rowSink struct {
	res *Result
}

func newSink(columns []string) *rowSink {
	return &rowSink{res: &Result{Columns: columns, Rows: [][]string{}, Nulls: [][]bool{}, RowLimit: RowLimit}}
}

// add stores a row and returns false once the limit is passed.
func (s *rowSink) add(vals []any) bool {
	if len(s.res.Rows) >= RowLimit {
		s.res.Truncated = true
		return false
	}
	row, nulls := make([]string, len(vals)), make([]bool, len(vals))
	for i, v := range vals {
		row[i], nulls[i] = cell(v)
	}
	s.res.Rows = append(s.res.Rows, row)
	s.res.Nulls = append(s.res.Nulls, nulls)
	return true
}
