package databases

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type pgConn struct{ c *pgx.Conn }

// OpenPostgres connects with pgx using a keyword/value string with every
// value quoted (never a URL, so the password cannot be mis-parsed).
func OpenPostgres(ctx context.Context, p Profile, password string) (Conn, error) {
	user := p.User
	if user == "" {
		user = "postgres"
	}
	db := p.Database
	if db == "" {
		db = user
	}
	// Loopback servers in containers rarely offer TLS; anything else is
	// asked for TLS first and falls back only if the server has none.
	dsn := kvDSN(map[string]string{
		"host": p.Host, "port": fmt.Sprint(p.Port), "user": user, "password": password, "dbname": db,
		"sslmode": "prefer", "connect_timeout": fmt.Sprint(int(ConnectTimeout.Seconds())),
		"application_name": "lucidbench",
	})
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	c, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &pgConn{c}, nil
}

func (p *pgConn) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = p.c.Close(ctx)
}

func (p *pgConn) Ping(ctx context.Context) error { return p.c.Ping(ctx) }

func (p *pgConn) Info(ctx context.Context) (Info, error) {
	var in Info
	err := p.c.QueryRow(ctx, `SELECT version(), pg_database_size(current_database())`).Scan(&in.Version, &in.SizeBytes)
	return in, err
}

func (p *pgConn) Schema(ctx context.Context) ([]Table, error) {
	tx, err := p.c.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Row counts: the planner's estimate, or the statistics collector's live
	// count while the table has never been analysed (reltuples is -1 then).
	rows, err := tx.Query(ctx, `
SELECT n.nspname, c.relname,
       CASE c.relkind WHEN 'v' THEN 'view' WHEN 'm' THEN 'view' ELSE 'table' END,
       CASE WHEN c.relkind IN ('v') THEN NULL
            WHEN c.reltuples >= 0 THEN c.reltuples::bigint
            ELSE s.n_live_tup END,
       a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod), NOT a.attnotnull
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
LEFT JOIN pg_stat_all_tables s ON s.relid = c.oid
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND n.nspname NOT LIKE 'pg_temp_%'
ORDER BY n.nspname, c.relname, a.attnum`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Table
	for rows.Next() {
		var schema, name, kind, col, typ string
		var est *int64
		var nullable bool
		if err := rows.Scan(&schema, &name, &kind, &est, &col, &typ, &nullable); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].Schema != schema || out[n-1].Name != name {
			out = append(out, Table{Schema: schema, Name: name, Kind: kind, Rows: est, Columns: []Column{}})
		}
		t := &out[len(out)-1]
		t.Columns = append(t.Columns, Column{Name: col, Type: typ, Nullable: nullable})
	}
	return out, rows.Err()
}

func (p *pgConn) Query(ctx context.Context, q string) (*Result, error) {
	if err := CheckSQL(q); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout+2*time.Second)
	defer cancel()
	began := time.Now()
	tx, err := p.c.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	// Rolling back, never committing: nothing a read does is kept.
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", QueryTimeout.Milliseconds())); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fds := rows.FieldDescriptions()
	cols := make([]string, len(fds))
	for i, f := range fds {
		cols[i] = f.Name
	}
	sink := newSink(cols)
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		if !sink.add(vals) {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sink.res.ElapsedMS = time.Since(began).Milliseconds()
	return sink.res, nil
}

// kvDSN builds a libpq keyword/value string, quoting every value.
func kvDSN(kv map[string]string) string {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(kv[k])
		fmt.Fprintf(&b, "%s='%s' ", k, v)
	}
	return strings.TrimSpace(b.String())
}
