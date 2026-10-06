package databases

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckSQLAllowsReads(t *testing.T) {
	for _, q := range []string{
		"SELECT 1",
		"  select * from t where name = 'a;b'",
		"SELECT 'it''s; fine'",
		`select "weird;col" from t`,
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"-- note\nSELECT 1",
		"/* drop table t; */ SELECT 1",
		"SELECT 1;",
		"SELECT 1 ;  ",
		"show tables",
		"EXPLAIN SELECT * FROM t",
		"(select 1) union (select 2)",
		"values (1), (2)",
		"table t",
		"describe t",
		"SELECT '{}'::jsonb #> '{a}'",
		"select `a;b` from t",
	} {
		if err := CheckSQL(q); err != nil {
			t.Errorf("CheckSQL(%q) refused a read: %v", q, err)
		}
	}
}

func TestCheckSQLRefusesWrites(t *testing.T) {
	for _, q := range []string{
		"INSERT INTO t VALUES (1)",
		"insert into t select 1",
		"UPDATE t SET a = 1",
		"DELETE FROM t",
		"DROP TABLE t",
		"TRUNCATE t",
		"CREATE TABLE x (a int)",
		"ALTER TABLE t ADD c int",
		"GRANT ALL ON t TO u",
		"COPY t TO '/tmp/x'",
		"CALL do_thing()",
		"SET ROLE admin",
		"BEGIN; INSERT INTO t VALUES (1); COMMIT",
		"SELECT 1; DROP TABLE t",
		"select 1;delete from t",
		"SELECT 1 INTO OUTFILE '/tmp/x'",
		"select * from t into dumpfile '/tmp/y'",
		"-- harmless\nDELETE FROM t",
		"/* x */ DROP TABLE t",
		// A backslash is an escape in MySQL and not in Postgres; both readings must pass.
		`SELECT '\'; DELETE FROM t; --'`,
		`SELECT 'a\\'; DROP TABLE t; --'`,
	} {
		err := CheckSQL(q)
		if !errors.Is(err, ErrReadOnly) {
			t.Errorf("CheckSQL(%q) = %v, want ErrReadOnly", q, err)
		}
	}
	if err := CheckSQL("  \n -- nothing\n"); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("a blank query: %v", err)
	}
}

func TestCheckRedisAllowlist(t *testing.T) {
	for _, line := range []string{
		"GET k", "get k", "SCAN 0 MATCH user:* COUNT 100", "TYPE k", "TTL k", "HGETALL h",
		"LRANGE l 0 10", "SMEMBERS s", "ZRANGE z 0 -1 WITHSCORES", "DBSIZE", `GET "a key"`,
	} {
		args, err := ParseRedisCommand(line)
		if err != nil {
			t.Errorf("parse %q: %v", line, err)
			continue
		}
		if err := CheckRedis(args); err != nil {
			t.Errorf("CheckRedis(%q) refused a read: %v", line, err)
		}
	}
	for _, line := range []string{
		"SET k v", "DEL k", "FLUSHALL", "FLUSHDB", "KEYS *", "CONFIG SET dir /tmp", "CONFIG GET *",
		"EVAL \"return 1\" 0", "SCRIPT FLUSH", "SHUTDOWN", "MIGRATE h 1 k 0 5", "SLAVEOF no one",
		"DEBUG SLEEP 1", "INCR k", "EXPIRE k 1", "RENAME a b", "SUBSCRIBE c", "MONITOR", "ACL SETUSER x on",
		"MODULE LOAD /x.so", "CLIENT KILL ID 1", "XADD s * a b", "PUBLISH c m", "OBJECT FREQ k", "SORT k STORE d",
	} {
		args, err := ParseRedisCommand(line)
		if err != nil {
			t.Errorf("parse %q: %v", line, err)
			continue
		}
		if err := CheckRedis(args); !errors.Is(err, ErrReadOnly) {
			t.Errorf("CheckRedis(%q) = %v, want ErrReadOnly", line, err)
		}
	}
	if err := CheckRedis(nil); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("empty: %v", err)
	}
}

func TestParseRedisCommand(t *testing.T) {
	for line, want := range map[string]string{
		`GET k`:                 `GET|k`,
		`  SET   "a b"  'c d' `: `SET|a b|c d`,
		`GET ""`:                `GET|`,
		`GET "a\"b"`:            `GET|a"b`,
		`GET a'b c'd`:           `GET|ab cd`,
	} {
		got, err := ParseRedisCommand(line)
		if err != nil || strings.Join(got, "|") != want {
			t.Errorf("ParseRedisCommand(%q) = %q, %v; want %q", line, got, err, want)
		}
	}
	if _, err := ParseRedisCommand(`GET "oops`); err == nil {
		t.Error("an unterminated quote is an error")
	}
}

func TestParseMongoQuery(t *testing.T) {
	ok := map[string]MongoQuery{
		`db.users.find({"a": 1})`:                             {Collection: "users", Filter: `{"a": 1}`, Limit: RowLimit},
		`db.users.find()`:                                     {Collection: "users", Filter: `{}`, Limit: RowLimit},
		`db.users.find({"a": {"$gt": 1}}, {"b": 1});`:         {Collection: "users", Filter: `{"a": {"$gt": 1}}`, Limit: RowLimit},
		`{"find": "orders", "filter": {"n": 2}, "limit": 20}`: {Collection: "orders", Filter: `{"n": 2}`, Limit: 20},
		`{"find": "orders", "limit": 99999}`:                  {Collection: "orders", Filter: `{}`, Limit: RowLimit},
		`{"collection": "orders"}`:                            {Collection: "orders", Filter: `{}`, Limit: RowLimit},
		`db.logs.find({"msg": "a, b"}, {"msg": 1})`:           {Collection: "logs", Filter: `{"msg": "a, b"}`, Limit: RowLimit},
	}
	for q, want := range ok {
		got, err := ParseMongoQuery(q)
		if err != nil || got != want {
			t.Errorf("ParseMongoQuery(%q) = %+v, %v; want %+v", q, got, err, want)
		}
	}
	for _, q := range []string{
		`db.users.insertOne({"a": 1})`,
		`db.users.deleteMany({})`,
		`db.dropDatabase()`,
		`db.users.aggregate([{"$out": "x"}])`,
		`{"delete": "users", "deletes": []}`,
		`{"insert": "users"}`,
		`db.users.find({"$where": "sleep(1000)"})`,
		`{"find": "users", "filter": {"$expr": {"$function": {"body": "x"}}}}`,
		`drop table users`,
		`db.users.find({}).forEach(function(d){})`,
	} {
		if _, err := ParseMongoQuery(q); err == nil {
			t.Errorf("ParseMongoQuery(%q) was accepted", q)
		}
	}
	if _, err := ParseMongoQuery(`db.users.deleteMany({})`); !errors.Is(err, ErrReadOnly) {
		t.Errorf("a write is ErrReadOnly: %v", err)
	}
}

func TestCellAndSinkLimits(t *testing.T) {
	if s, null := cell(nil); !null || s != "" {
		t.Error("nil is NULL")
	}
	if s, _ := cell([]byte{0xff, 0xfe}); s != `\xfffe` {
		t.Errorf("binary = %q", s)
	}
	if s, _ := cell([16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 1, 2, 3, 4, 5, 6, 7, 8}); s != "12345678-9abc-def0-0102-030405060708" {
		t.Errorf("uuid = %q", s)
	}
	if s, _ := cell(time.Date(2026, 10, 6, 1, 2, 3, 0, time.UTC)); s != "2026-10-06T01:02:03Z" {
		t.Errorf("time = %q", s)
	}
	if s, _ := cell(strings.Repeat("é", 5000)); len(s) > maxCellBytes+4 || !strings.HasSuffix(s, "…") {
		t.Errorf("a long cell is cut on a rune boundary: %d bytes", len(s))
	}
	sink := newSink([]string{"n"})
	for i := 0; i < RowLimit+50; i++ {
		if !sink.add([]any{i}) {
			break
		}
	}
	if len(sink.res.Rows) != RowLimit || !sink.res.Truncated {
		t.Errorf("rows = %d truncated = %v, want %d and true", len(sink.res.Rows), sink.res.Truncated, RowLimit)
	}
}
