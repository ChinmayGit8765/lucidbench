package databases

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisAllowed are the commands the Redis query box runs: reads only. KEYS
// is left out because it blocks the server; use SCAN.
var redisAllowed = map[string]bool{
	"GET": true, "MGET": true, "STRLEN": true, "TYPE": true, "TTL": true, "PTTL": true, "EXISTS": true,
	"SCAN": true, "HSCAN": true, "SSCAN": true, "ZSCAN": true,
	"HGET": true, "HGETALL": true, "HKEYS": true, "HVALS": true, "HLEN": true, "HMGET": true, "HEXISTS": true,
	"LRANGE": true, "LLEN": true, "LINDEX": true,
	"SMEMBERS": true, "SCARD": true, "SISMEMBER": true, "SRANDMEMBER": true,
	"ZRANGE": true, "ZCARD": true, "ZSCORE": true, "ZRANK": true, "ZCOUNT": true,
	"DBSIZE": true, "PING": true, "INFO": true, "EXPIRETIME": true,
}

// RedisAllowed lists the allowed commands, sorted.
func RedisAllowed() []string {
	out := make([]string, 0, len(redisAllowed))
	for c := range redisAllowed {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// ParseRedisCommand splits a command line the way redis-cli does: words
// separated by spaces, with single or double quotes around words that hold
// spaces, and \" or \\ inside double quotes.
func ParseRedisCommand(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	in, quote, started := false, byte(0), false
	flush := func() {
		if started {
			args = append(args, cur.String())
			cur.Reset()
			started = false
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case in && quote == '"' && c == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case in && c == quote:
			in = false
		case in:
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			in, quote, started = true, c, true
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			flush()
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	if in {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return args, nil
}

// CheckRedis refuses anything but an allowed read command.
func CheckRedis(args []string) error {
	if len(args) == 0 {
		return ErrEmptyQuery
	}
	cmd := strings.ToUpper(args[0])
	if !redisAllowed[cmd] {
		return fmt.Errorf("%w (%s is not an allowed command; reads only, such as GET, SCAN, TYPE, TTL, HGETALL, LRANGE)", ErrReadOnly, cmd)
	}
	return nil
}

type redisConn struct{ c *redis.Client }

// OpenRedis connects with go-redis (RESP2, so replies are flat lists).
func OpenRedis(ctx context.Context, p Profile, password string) (Conn, error) {
	opt := &redis.Options{
		Addr:     net.JoinHostPort(strings.Trim(p.Host, "[]"), strconv.Itoa(p.Port)),
		Username: p.User, Password: password,
		Protocol:    2,
		DialTimeout: ConnectTimeout, ReadTimeout: QueryTimeout, WriteTimeout: QueryTimeout,
		MaxRetries: -1, DisableIdentity: true,
	}
	if n, err := strconv.Atoi(p.Database); err == nil {
		opt.DB = n
	}
	return &redisConn{redis.NewClient(opt)}, nil
}

func (r *redisConn) Close() { _ = r.c.Close() }

func (r *redisConn) Ping(ctx context.Context) error { return r.c.Ping(ctx).Err() }

func (r *redisConn) Info(ctx context.Context) (Info, error) {
	in := Info{SizeBytes: -1}
	s, err := r.c.Info(ctx, "server", "memory").Result()
	if err != nil {
		return in, err
	}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		switch {
		case ok && k == "redis_version":
			in.Version = "Redis " + v
		case ok && k == "used_memory":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				in.SizeBytes = n
			}
		}
	}
	return in, nil
}

// Schema lists up to 200 keys (a SCAN, so it never blocks the server) with
// their type and length, as the closest thing Redis has to tables.
func (r *redisConn) Schema(ctx context.Context) ([]Table, error) {
	const max = 200
	var out []Table
	var cursor uint64
	for len(out) < max {
		keys, next, err := r.c.Scan(ctx, cursor, "*", 100).Result()
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if len(out) >= max {
				break
			}
			t, err := r.c.Type(ctx, k).Result()
			if err != nil {
				return nil, err
			}
			tb := Table{Name: k, Kind: t, Columns: []Column{}}
			var n int64 = -1
			switch t {
			case "list":
				n, _ = r.c.LLen(ctx, k).Result()
			case "hash":
				n, _ = r.c.HLen(ctx, k).Result()
			case "set":
				n, _ = r.c.SCard(ctx, k).Result()
			case "zset":
				n, _ = r.c.ZCard(ctx, k).Result()
			case "string":
				n, _ = r.c.StrLen(ctx, k).Result()
			}
			if n >= 0 {
				tb.Rows = &n
			}
			out = append(out, tb)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *redisConn) Query(ctx context.Context, q string) (*Result, error) {
	args, err := ParseRedisCommand(q)
	if err != nil {
		return nil, err
	}
	if err := CheckRedis(args); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	began := time.Now()
	argv := make([]any, len(args))
	for i, a := range args {
		argv[i] = a
	}
	v, err := r.c.Do(ctx, argv...).Result()
	if err == redis.Nil {
		v, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	sink := newSink([]string{"value"})
	switch x := v.(type) {
	case []any:
		// HGETALL and the *SCAN pairs read better as they come; one element per row.
		for _, e := range x {
			if !sink.add([]any{flatten(e)}) {
				break
			}
		}
	default:
		sink.add([]any{flatten(x)})
	}
	sink.res.ElapsedMS = time.Since(began).Milliseconds()
	return sink.res, nil
}

// flatten renders nested replies (the SCAN cursor and its keys) as one cell.
func flatten(v any) any {
	if l, ok := v.([]any); ok {
		parts := make([]string, len(l))
		for i, e := range l {
			s, _ := cell(flatten(e))
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return v
}
