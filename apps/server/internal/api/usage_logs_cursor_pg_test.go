package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

func TestUsageLogsPageByCursor(t *testing.T) {
	m := newMePG(t)
	ctx := context.Background()
	_, adminCookie := m.signedIn("admin")
	_, memberCookie := m.signedIn("user")
	user, _ := m.signedIn("user")
	tag := uuid.NewString()[:8]
	modelA, modelB := "cursor-a-"+tag, "cursor-b-"+tag
	t.Cleanup(func() {
		_, _ = m.pool.Exec(context.Background(), "DELETE FROM usage_logs WHERE model_id = ANY($1)", []string{modelA, modelB})
	})

	// 30 rows, every one of them a pair with the same instant as the next, so
	// the page boundaries fall between rows that only the id tells apart.
	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	for i := 0; i < 30; i++ {
		model, status := modelA, "ok"
		if i%3 == 0 {
			model, status = modelB, "error"
		}
		code := "model_error"
		if _, err := m.q.InsertUsageLog(ctx, sqlc.InsertUsageLogParams{
			CreatedAt: pgtype.Timestamptz{Time: base.Add(time.Duration(i/2) * time.Second), Valid: true},
			Kind:      "chat", Status: status, ErrorCode: map[bool]*string{true: &code}[status == "error"],
			UserID: &user.ID, ChannelName: "cursor channel", ModelID: model, InputTokens: 10, OutputTokens: 5,
		}); err != nil {
			t.Fatal(err)
		}
	}

	page := func(query url.Values) (int, UsageLogPage) {
		t.Helper()
		res, raw := m.do(adminCookie, http.MethodGet, "/api/v1/admin/usage-logs?"+query.Encode(), "", nil)
		var out UsageLogPage
		if res.StatusCode == http.StatusOK {
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode, out
	}
	// walk reads every page of a filter and returns the rows in the order met.
	walk := func(filter url.Values, limit int) (rows []UsageLog, pages int, summary UsageSummary) {
		t.Helper()
		cursor := ""
		for {
			q := url.Values{"limit": {fmt.Sprint(limit)}}
			for k, v := range filter {
				q[k] = v
			}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			code, p := page(q)
			if code != http.StatusOK {
				t.Fatalf("page %d: %d", pages+1, code)
			}
			pages++
			if pages == 1 {
				summary = p.Summary
			} else if !reflect.DeepEqual(p.Summary, summary) {
				t.Fatalf("the summary changed from page to page: %+v vs %+v", p.Summary, summary)
			}
			if len(p.Items) > limit {
				t.Fatalf("%d rows on a page of %d", len(p.Items), limit)
			}
			rows = append(rows, p.Items...)
			if p.NextCursor == nil {
				return
			}
			if len(p.Items) != limit {
				t.Fatalf("a short page (%d of %d) has a next cursor", len(p.Items), limit)
			}
			cursor = *p.NextCursor
			if pages > 100 {
				t.Fatal("the cursor never ends")
			}
		}
	}
	checkWalk := func(name string, rows []UsageLog, total int) {
		t.Helper()
		if len(rows) != total {
			t.Fatalf("%s: %d rows, want %d", name, len(rows), total)
		}
		seen := map[uuid.UUID]bool{}
		for i, r := range rows {
			if seen[r.Id] {
				t.Fatalf("%s: row %s on two pages", name, r.Id)
			}
			seen[r.Id] = true
			if i == 0 {
				continue
			}
			prev := rows[i-1]
			// Newest first; within one instant, by id descending.
			if r.CreatedAt.After(prev.CreatedAt) || (r.CreatedAt.Equal(prev.CreatedAt) && r.Id.String() > prev.Id.String()) {
				t.Fatalf("%s: out of order at %d: %v %s after %v %s", name, i, r.CreatedAt, r.Id, prev.CreatedAt, prev.Id)
			}
		}
	}

	// Everything of this test, in pages of 7 (4 full pages and a short one).
	rows, pages, sum := walk(url.Values{"userId": {user.ID.String()}}, 7)
	checkWalk("all", rows, 30)
	if pages != 5 || sum.Requests != 30 || sum.Errors != 10 {
		t.Errorf("%d pages, summary %+v", pages, sum)
	}
	// A page exactly the size of what is left ends without a phantom next page.
	_, exact, _ := walk(url.Values{"userId": {user.ID.String()}}, 30)
	if exact != 1 {
		t.Errorf("a page of exactly all the rows took %d pages", exact)
	}

	// Filters hold from one page to the next: the cursor does not widen them.
	rows, _, sum = walk(url.Values{"userId": {user.ID.String()}, "model": {modelB}}, 4)
	checkWalk("model", rows, 10)
	for _, r := range rows {
		if r.Model != modelB || r.Status != UsageStatus("error") {
			t.Fatalf("a row outside the filter: %+v", r)
		}
	}
	if sum.Requests != 10 || sum.Errors != 10 {
		t.Errorf("filtered summary: %+v", sum)
	}
	rows, _, sum = walk(url.Values{"userId": {user.ID.String()}, "status": {"ok"}}, 6)
	checkWalk("status", rows, 20)
	if sum.Requests != 20 || sum.Errors != 0 {
		t.Errorf("status summary: %+v", sum)
	}
	// A time range cuts the same way on every page.
	from := base.Add(5 * time.Second).Format(time.RFC3339Nano) // pairs 5.. of 0..14
	rows, _, sum = walk(url.Values{"userId": {user.ID.String()}, "from": {from}}, 5)
	for _, r := range rows {
		if r.CreatedAt.Before(base.Add(5 * time.Second)) {
			t.Fatalf("a row before the range: %v", r.CreatedAt)
		}
	}
	checkWalk("from", rows, 20)
	if sum.Requests != 20 {
		t.Errorf("range summary: %+v", sum)
	}

	// Walking from the middle: the rows after a cursor are exactly the rest.
	_, firstPage := page(url.Values{"userId": {user.ID.String()}, "limit": {"10"}})
	_, rest := page(url.Values{"userId": {user.ID.String()}, "limit": {"100"}, "cursor": {*firstPage.NextCursor}})
	if len(rest.Items) != 20 || rest.NextCursor != nil || rest.Items[0].Id == firstPage.Items[9].Id {
		t.Fatalf("the rest after 10: %d rows, next %v", len(rest.Items), rest.NextCursor)
	}

	// A cursor that is not one is the client's mistake, and the page is for admins.
	for _, bad := range []string{"not-base64!", "Zm9v", "Zm9vfGJhcg"} {
		if code, _ := page(url.Values{"cursor": {bad}}); code != http.StatusBadRequest {
			t.Errorf("cursor %q: %d, want 400", bad, code)
		}
	}
	if res, _ := m.do(memberCookie, http.MethodGet, "/api/v1/admin/usage-logs", "", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("a member: %d, want 404", res.StatusCode)
	}
}
