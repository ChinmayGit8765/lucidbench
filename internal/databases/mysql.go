package databases

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
)

type myConn struct{ db *sql.DB }

// OpenMySQL connects to MySQL or MariaDB with go-sql-driver.
func OpenMySQL(ctx context.Context, p Profile, password string) (Conn, error) {
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd = p.User, password
	if cfg.User == "" {
		cfg.User = "root"
	}
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	cfg.DBName = p.Database
	cfg.Timeout = ConnectTimeout
	cfg.ReadTimeout = QueryTimeout + 5*time.Second
	cfg.AllowNativePasswords = true
	cfg.TLSConfig = "preferred"
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Minute)
	return &myConn{db}, nil
}

func (m *myConn) Close() { _ = m.db.Close() }

func (m *myConn) Ping(ctx context.Context) error { return m.db.PingContext(ctx) }

func (m *myConn) Info(ctx context.Context) (Info, error) {
	in := Info{SizeBytes: -1}
	if err := m.db.QueryRowContext(ctx, `SELECT VERSION()`).Scan(&in.Version); err != nil {
		return in, err
	}
	var size sql.NullInt64
	if err := m.db.QueryRowContext(ctx, `SELECT CAST(SUM(data_length + index_length) AS SIGNED) FROM information_schema.tables WHERE table_schema = DATABASE()`).Scan(&size); err == nil && size.Valid {
		in.SizeBytes = size.Int64
	}
	return in, nil
}

func (m *myConn) Schema(ctx context.Context) ([]Table, error) {
	tx, err := m.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
SELECT t.table_schema, t.table_name, t.table_type, t.table_rows, c.column_name, c.column_type, c.is_nullable
FROM information_schema.tables t
JOIN information_schema.columns c ON c.table_schema = t.table_schema AND c.table_name = t.table_name
WHERE t.table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys')
ORDER BY t.table_schema, t.table_name, c.ordinal_position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Table
	for rows.Next() {
		var schema, name, kind, col, typ, null string
		var est sql.NullInt64
		if err := rows.Scan(&schema, &name, &kind, &est, &col, &typ, &null); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].Schema != schema || out[n-1].Name != name {
			t := Table{Schema: schema, Name: name, Kind: "table", Columns: []Column{}}
			if kind == "VIEW" {
				t.Kind = "view"
			} else if est.Valid {
				v := est.Int64
				t.Rows = &v
			}
			out = append(out, t)
		}
		t := &out[len(out)-1]
		t.Columns = append(t.Columns, Column{Name: col, Type: typ, Nullable: null == "YES"})
	}
	return out, rows.Err()
}

func (m *myConn) Query(ctx context.Context, q string) (*Result, error) {
	res, err := m.query(ctx, q)
	var me *mysql.MySQLError
	// 1792: cannot execute in a READ ONLY transaction; 1290: --read-only server option.
	if errors.As(err, &me) && (me.Number == 1792 || me.Number == 1290) {
		return nil, fmt.Errorf("%w (the database refused it: %s)", ErrReadOnly, me.Message)
	}
	return res, err
}

func (m *myConn) query(ctx context.Context, q string) (*Result, error) {
	if err := CheckSQL(q); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout+2*time.Second)
	defer cancel()
	began := time.Now()
	c, err := m.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	// MAX_EXECUTION_TIME applies to SELECT only (MariaDB calls it
	// max_statement_time); the context deadline above covers the rest.
	if _, err := c.ExecContext(ctx, fmt.Sprintf("SET SESSION max_execution_time = %d", QueryTimeout.Milliseconds())); err != nil {
		_, _ = c.ExecContext(ctx, fmt.Sprintf("SET SESSION max_statement_time = %d", int(QueryTimeout.Seconds())))
	}
	tx, err := c.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	sink := newSink(cols)
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
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
