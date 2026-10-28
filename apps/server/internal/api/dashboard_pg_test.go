package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/db/dbtest"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// The dashboard reads the usage ledger, so the rows here are written straight
// into it: known tokens, prices and latencies, from which every figure of the
// page can be worked out by hand.
//
// The page sums the whole ledger, and the other tests' runs write to it from
// background goroutines that can outlive them, so this test has a database of
// its own with an empty ledger. Counting what it inserted is then exact; the
// before/after differences and the lookups by this run's models and channels
// remain as a second line of defence for a ledger somebody wrote to anyway.
func TestAdminDashboardFigures(t *testing.T) {
	pool := dbtest.Pool(t, "api_dashboard")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DELETE FROM usage_logs"); err != nil {
		t.Fatalf("empty the ledger: %v", err)
	}
	q := sqlc.New(pool)
	tag := uuid.NewString()[:8]
	modelA, modelB, modelC := "dash-a-"+tag, "dash-b-"+tag, "dash-c-"+tag
	chanA, chanB := "dash channel a "+tag, "dash channel b "+tag

	newUser := func(role string) sqlc.User {
		var u sqlc.User
		id := uuid.NewString()
		if err := pool.QueryRow(ctx, `INSERT INTO users (email, name, role) VALUES ($1, $2, $3::user_role) RETURNING id, email, name, role`,
			id+"@test.invalid", role+"-"+tag, role).Scan(&u.ID, &u.Email, &u.Name, &u.Role); err != nil {
			t.Fatal(err)
		}
		return u
	}
	admin, member := newUser("admin"), newUser("user")
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM usage_logs WHERE model_id = ANY($1)", []string{modelA, modelB, modelC})
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE id = ANY($1)", []uuid.UUID{admin.ID, member.ID})
	})

	cfg := &config.Config{CORSOrigins: []string{"http://localhost"}}
	web := httptest.NewServer(NewRouter(cfg, NewServer(cfg, pool, nil, nil), auth.NewMiddleware(q)))
	t.Cleanup(web.Close)
	session := func(user sqlc.User) *http.Cookie {
		token, hash, err := auth.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.CreateSession(ctx, sqlc.CreateSessionParams{UserID: user.ID, TokenHash: hash,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}}); err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: auth.CookieName, Value: token}
	}
	adminCookie, memberCookie := session(admin), session(member)
	get := func(cookie *http.Cookie, rng string) (int, AdminDashboard) {
		t.Helper()
		query := url.Values{}
		if rng != "" {
			query.Set("range", rng)
		}
		req, _ := http.NewRequest(http.MethodGet, web.URL+"/api/v1/admin/dashboard?"+query.Encode(), nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out AdminDashboard
		if res.StatusCode == http.StatusOK {
			if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode, out
	}

	if code, _ := get(memberCookie, ""); code != http.StatusNotFound {
		t.Errorf("member: %d, want 404", code)
	}
	if code, _ := get(nil, ""); code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d, want 401", code)
	}
	if code, _ := get(adminCookie, "yesterday"); code != http.StatusBadRequest {
		t.Errorf("bad range: %d, want 400", code)
	}
	_, before := get(adminCookie, "7d")
	_, before24 := get(adminCookie, "24h")

	const reps = 6
	type row struct {
		age                          time.Duration
		user                         uuid.UUID
		model, channel, status, code string
		in, out, read, write         int
		inPrice, readPrice, cost     *float64
		firstToken                   *int
		duration                     int
	}
	f := func(v float64) *float64 { return &v }
	ms := func(v int) *int { return &v }
	hour := time.Hour
	rows := []row{
		// A on channel A, the admin's: a cache hit, a miss that wrote the cache, a failure.
		{age: hour, user: admin.ID, model: modelA, channel: chanA, status: "ok", in: 600, out: 200, read: 400,
			inPrice: f(3), readPrice: f(0.3), cost: f(0.01), firstToken: ms(100), duration: 1000},
		{age: 2 * hour, user: admin.ID, model: modelA, channel: chanA, status: "ok", in: 1000, out: 100, write: 1000,
			inPrice: f(3), cost: f(0.02), firstToken: ms(300), duration: 3000},
		{age: 3 * hour, user: admin.ID, model: modelA, channel: chanA, status: "error", code: "model_auth", duration: 50},
		// B on channel B, the member's: mostly cached.
		{age: hour, user: member.ID, model: modelB, channel: chanB, status: "ok", in: 200, out: 50, read: 800,
			inPrice: f(3), readPrice: f(0.3), cost: f(0.03), firstToken: ms(500), duration: 2500},
		// C: nothing was sent, so there is no rate to speak of.
		{age: hour, user: member.ID, model: modelC, channel: chanB, status: "ok", duration: 10},
		// A request from the window before this one.
		{age: 8 * 24 * hour, user: admin.ID, model: modelA, channel: chanA, status: "ok", in: 100, out: 10, cost: f(0.5),
			firstToken: ms(50), duration: 400},
	}
	// Every row `reps` times, so that these models rank among the busiest even
	// in a database that other tests have left rows in.
	var each []row
	for _, r := range rows {
		for range reps {
			each = append(each, r)
		}
	}
	for _, r := range each {
		var code *string
		if r.code != "" {
			code = &r.code
		}
		if _, err := pool.Exec(ctx, `INSERT INTO usage_logs (created_at, kind, status, error_code, user_id, channel_name, model_id,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, input_price, cache_read_price, cost, first_token_ms, duration_ms)
			VALUES ($1, 'chat', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			time.Now().Add(-r.age), r.status, code, r.user, r.channel, r.model,
			r.in, r.out, r.read, r.write, r.inPrice, r.readPrice, r.cost, r.firstToken, r.duration); err != nil {
			t.Fatal(err)
		}
	}

	near := func(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 1e-9 }

	code, after := get(adminCookie, "7d")
	if code != http.StatusOK {
		t.Fatalf("7d: %d", code)
	}
	if after.Range != DashboardRange("7d") || after.Bucket != "day" || len(after.Trend) != 7 {
		t.Errorf("window: %s %s, %d trend points", after.Range, after.Bucket, len(after.Trend))
	}
	cur, was := after.Current, before.Current
	if cur.Requests-was.Requests != 5*reps || cur.Errors-was.Errors != reps {
		t.Errorf("requests +%d errors +%d, want +30 +6", cur.Requests-was.Requests, cur.Errors-was.Errors)
	}
	if cur.InputTokens-was.InputTokens != 1800*reps || cur.OutputTokens-was.OutputTokens != 350*reps ||
		cur.CacheReadTokens-was.CacheReadTokens != 1200*reps || cur.CacheWriteTokens-was.CacheWriteTokens != 1000*reps {
		t.Errorf("tokens in +%d out +%d read +%d write +%d", cur.InputTokens-was.InputTokens, cur.OutputTokens-was.OutputTokens,
			cur.CacheReadTokens-was.CacheReadTokens, cur.CacheWriteTokens-was.CacheWriteTokens)
	}
	if d := cur.Cost - was.Cost; math.Abs(d-0.06*reps) > 1e-9 {
		t.Errorf("cost +%v, want +0.36", d)
	}
	if cur.ActiveUsers < 2 {
		t.Errorf("active users = %d", cur.ActiveUsers)
	}
	if after.Previous.Requests-before.Previous.Requests != reps {
		t.Errorf("previous window +%d requests, want +6", after.Previous.Requests-before.Previous.Requests)
	}
	if cur.CacheHitRate == nil || cur.CacheSavings == nil || cur.FirstTokenP50Ms == nil || cur.FirstTokenP95Ms == nil {
		t.Errorf("kpis = %+v", cur)
	}
	var trendRequests, trendWas int64
	for _, p := range after.Trend {
		trendRequests += p.Requests
	}
	for _, p := range before.Trend {
		trendWas += p.Requests
	}
	if trendRequests-trendWas != 5*reps {
		t.Errorf("trend holds +%d requests, want +30", trendRequests-trendWas)
	}

	// The rate is token-weighted, over successful requests: A read 400 of
	// (1000 + 2000) sent; the failure adds nothing.
	byModel := map[string]DashboardModel{}
	for _, m := range after.Models {
		byModel[m.Model] = m
	}
	a, b, c := byModel[modelA], byModel[modelB], byModel[modelC]
	if a.Requests != 3*reps || a.Errors != reps || a.CacheReadTokens != 400*reps || !near(a.CacheHitRate, 400.0/3000) ||
		!near(a.FirstTokenP50Ms, 200) || !near(&a.Cost, 0.03*reps) || a.InputTokens != 1600*reps || a.OutputTokens != 300*reps {
		t.Errorf("model A = %+v", a)
	}
	if b.Requests != reps || !near(b.CacheHitRate, 0.8) || !near(b.FirstTokenP50Ms, 500) {
		t.Errorf("model B = %+v", b)
	}
	if c.Requests != reps || c.CacheHitRate != nil || c.FirstTokenP50Ms != nil {
		t.Errorf("model C = %+v: no tokens sent and no first token, so no rate and no latency", c)
	}

	// Savings: what the reads would have cost at the input price, less what
	// they cost, for the two requests priced both ways.
	if d := *cur.CacheSavings - func() float64 {
		if was.CacheSavings == nil {
			return 0
		}
		return *was.CacheSavings
	}(); math.Abs(d-reps*(400+800)*(3-0.3)/1e6) > 1e-12 {
		t.Errorf("savings +%v", d)
	}

	byChannel := map[string]DashboardChannel{}
	for _, ch := range after.Channels {
		byChannel[ch.ChannelName] = ch
	}
	ca, cb := byChannel[chanA], byChannel[chanB]
	if ca.Requests != 3*reps || ca.Errors != reps || ca.LastErrorAt == nil || ca.ChannelId != nil ||
		!near(ca.FirstTokenP50Ms, 200) || !near(ca.FirstTokenP95Ms, 300) || !near(ca.DurationP50Ms, 2000) || !near(ca.DurationP95Ms, 3000) {
		t.Errorf("channel A = %+v", ca)
	}
	if cb.Requests != 2*reps || cb.Errors != 0 || cb.LastErrorAt != nil || !near(cb.FirstTokenP50Ms, 500) {
		t.Errorf("channel B = %+v", cb)
	}

	byUser := map[uuid.UUID]DashboardUser{}
	for _, u := range after.Users {
		if u.UserId != nil {
			byUser[*u.UserId] = u
		}
	}
	if u := byUser[admin.ID]; u.Requests != 3*reps || !near(&u.Cost, 0.03*reps) || u.UserName == nil || *u.UserName != admin.Name {
		t.Errorf("admin = %+v", u)
	}
	if u := byUser[member.ID]; u.Requests != 2*reps || !near(&u.Cost, 0.03*reps) {
		t.Errorf("member = %+v", u)
	}

	var failure *DashboardError
	for i := range after.Errors {
		if after.Errors[i].Model == modelA {
			failure = &after.Errors[i]
		}
	}
	if failure == nil || failure.ChannelName != chanA || failure.ErrorCode == nil || *failure.ErrorCode != "model_auth" {
		t.Errorf("errors = %+v", after.Errors)
	}
	for i := 1; i < len(after.Errors); i++ {
		if after.Errors[i].CreatedAt.After(after.Errors[i-1].CreatedAt) {
			t.Errorf("errors not newest first at %d", i)
		}
	}

	// The hourly view: 24 buckets, the same rows (all within the last 24 hours
	// but the old one).
	_, day := get(adminCookie, "24h")
	if day.Bucket != "hour" || len(day.Trend) < 24 || len(day.Trend) > 25 {
		t.Errorf("24h: bucket %s, %d points", day.Bucket, len(day.Trend))
	}
	if day.Current.Requests-before24.Current.Requests != 5*reps {
		t.Errorf("24h: +%d requests, want +30", day.Current.Requests-before24.Current.Requests)
	}
}
