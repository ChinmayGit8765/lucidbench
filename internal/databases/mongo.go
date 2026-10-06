package databases

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoQuery is a parsed find.
type MongoQuery struct {
	Collection string
	Filter     string // extended JSON
	Limit      int
}

var (
	mongoShellRE = regexp.MustCompile(`(?s)^db\.([A-Za-z0-9_.-]+)\.find\((.*)\)\s*;?$`)
	collRE       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,119}$`)
)

// jsFilterOps run JavaScript on the server, which is not a read the query
// box vouches for.
var jsFilterOps = []string{"$where", "$function", "$accumulator"}

// ParseMongoQuery accepts either {"find": "<collection>", "filter": {...},
// "limit": N} or the shell form db.<collection>.find({...}). It is always a
// find; the limit is capped at RowLimit.
func ParseMongoQuery(q string) (MongoQuery, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return MongoQuery{}, ErrEmptyQuery
	}
	var mq MongoQuery
	if m := mongoShellRE.FindStringSubmatch(q); m != nil {
		mq.Collection, mq.Filter = m[1], strings.TrimSpace(m[2])
		// find(filter, projection): only the filter is read.
		if i := splitTopLevel(mq.Filter); i >= 0 {
			mq.Filter = strings.TrimSpace(mq.Filter[:i])
		}
	} else if strings.HasPrefix(q, "{") {
		var raw struct {
			Find       string          `json:"find"`
			Collection string          `json:"collection"`
			Filter     json.RawMessage `json:"filter"`
			Limit      int             `json:"limit"`
		}
		if err := json.Unmarshal([]byte(q), &raw); err != nil {
			return mq, fmt.Errorf(`not a find: write {"find": "collection", "filter": {}, "limit": 20} or db.collection.find({})`)
		}
		mq.Collection = raw.Find
		if mq.Collection == "" {
			mq.Collection = raw.Collection
		}
		mq.Filter, mq.Limit = string(raw.Filter), raw.Limit
	} else {
		return mq, fmt.Errorf("%w (only find is supported: db.collection.find({}) or {\"find\": \"collection\"})", ErrReadOnly)
	}
	if !collRE.MatchString(mq.Collection) {
		return mq, fmt.Errorf("collection name is missing or not valid")
	}
	if strings.TrimSpace(mq.Filter) == "" || mq.Filter == "null" {
		mq.Filter = "{}"
	}
	if !json.Valid([]byte(mq.Filter)) {
		return mq, fmt.Errorf("the filter is not valid JSON (only a plain find is supported)")
	}
	low := strings.ToLower(mq.Filter)
	for _, op := range jsFilterOps {
		if strings.Contains(low, op) {
			return mq, fmt.Errorf("%w (%s runs JavaScript on the server)", ErrReadOnly, op)
		}
	}
	if mq.Limit <= 0 || mq.Limit > RowLimit {
		mq.Limit = RowLimit
	}
	return mq, nil
}

// splitTopLevel returns the index of the first comma outside braces,
// brackets and strings, or -1.
func splitTopLevel(s string) int {
	depth, in := 0, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case in != 0:
			if c == '\\' {
				i++
			} else if c == in {
				in = 0
			}
		case c == '"' || c == '\'':
			in = c
		case c == '{' || c == '[' || c == '(':
			depth++
		case c == '}' || c == ']' || c == ')':
			depth--
		case c == ',' && depth == 0:
			return i
		}
	}
	return -1
}

type mongoConn struct {
	c  *mongo.Client
	db string
}

// OpenMongo connects with the official driver. The credential is set on the
// options, so the password is not in a URI.
func OpenMongo(ctx context.Context, p Profile, password string) (Conn, error) {
	opt := options.Client().
		SetHosts([]string{net.JoinHostPort(strings.Trim(p.Host, "[]"), strconv.Itoa(p.Port))}).
		SetAppName("lucidbench").
		SetConnectTimeout(ConnectTimeout).
		SetServerSelectionTimeout(ConnectTimeout).
		SetDirect(true)
	db := p.Database
	if p.User != "" {
		src := "admin"
		opt.SetAuth(options.Credential{Username: p.User, Password: password, AuthSource: src})
	}
	if db == "" {
		db = "admin"
	}
	c, err := mongo.Connect(opt)
	if err != nil {
		return nil, err
	}
	return &mongoConn{c, db}, nil
}

func (m *mongoConn) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = m.c.Disconnect(ctx)
}

func (m *mongoConn) Ping(ctx context.Context) error { return m.c.Ping(ctx, nil) }

func (m *mongoConn) Info(ctx context.Context) (Info, error) {
	in := Info{SizeBytes: -1}
	var bi struct {
		Version string `bson:"version"`
	}
	if err := m.c.Database(m.db).RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&bi); err != nil {
		return in, err
	}
	in.Version = "MongoDB " + bi.Version
	var st struct {
		DataSize  float64 `bson:"dataSize"`
		IndexSize float64 `bson:"indexSize"`
	}
	if err := m.c.Database(m.db).RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}}).Decode(&st); err == nil {
		in.SizeBytes = int64(st.DataSize + st.IndexSize)
	}
	return in, nil
}

// Schema lists the collections with an estimated document count and the
// field names of one sampled document.
func (m *mongoConn) Schema(ctx context.Context) ([]Table, error) {
	db := m.c.Database(m.db)
	specs, err := db.ListCollectionSpecifications(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	out := []Table{}
	for _, s := range specs {
		if strings.HasPrefix(s.Name, "system.") {
			continue
		}
		t := Table{Name: s.Name, Kind: "collection", Columns: []Column{}}
		if s.Type == "view" {
			t.Kind = "view"
		}
		coll := db.Collection(s.Name)
		if t.Kind == "collection" {
			if n, err := coll.EstimatedDocumentCount(ctx); err == nil {
				t.Rows = &n
			}
		}
		var doc bson.D
		if err := coll.FindOne(ctx, bson.D{}).Decode(&doc); err == nil {
			for _, e := range doc {
				t.Columns = append(t.Columns, Column{Name: e.Key, Type: fmt.Sprintf("%T", e.Value)})
			}
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *mongoConn) Query(ctx context.Context, q string) (*Result, error) {
	mq, err := ParseMongoQuery(q)
	if err != nil {
		return nil, err
	}
	var filter bson.D
	if err := bson.UnmarshalExtJSON([]byte(mq.Filter), false, &filter); err != nil {
		return nil, fmt.Errorf("the filter is not valid JSON: %v", err)
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	began := time.Now()
	cur, err := m.c.Database(m.db).Collection(mq.Collection).Find(ctx, filter, options.Find().SetLimit(int64(mq.Limit+1)))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	sink := newSink([]string{"document"})
	for cur.Next(ctx) {
		var doc bson.D
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		b, err := bson.MarshalExtJSON(doc, false, false)
		if err != nil {
			return nil, err
		}
		if len(sink.res.Rows) >= mq.Limit {
			sink.res.Truncated = true
			break
		}
		sink.add([]any{string(b)})
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	sink.res.RowLimit = mq.Limit
	sink.res.ElapsedMS = time.Since(began).Milliseconds()
	return sink.res, nil
}
