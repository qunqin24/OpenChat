package api

import (
	"errors"
	"math/big"
	"net/http"
	"testing"
	"time"
	// The zone database, as the binary carries it: these tests read zones by
	// name and must not depend on the machine they run on having one.
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return zone
}

// Daily and monthly caps start over at local midnight of the server's zone,
// not UTC's.
func TestCapacityWindows(t *testing.T) {
	shanghai := mustZone(t, "Asia/Shanghai")
	newYork := mustZone(t, "America/New_York")
	reset := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		rule   capacityRule
		now    time.Time
		want   time.Time // zero is "all history"
		wantAt string    // the same instant in UTC, so a failure reads without arithmetic
	}{
		{
			name: "daily starts at the zone's midnight",
			rule: capacityRule{period: Daily},
			now:  time.Date(2026, 10, 15, 10, 30, 0, 0, shanghai),
			want: time.Date(2026, 10, 15, 0, 0, 0, 0, shanghai), wantAt: "2026-10-14T16:00:00Z",
		},
		{
			// 17:30 UTC on the 14th is already the 15th in Shanghai: the day is
			// the zone's, so a UTC reading would start it a day early.
			name: "daily is the local day while UTC is still the day before",
			rule: capacityRule{period: Daily},
			now:  time.Date(2026, 10, 14, 17, 30, 0, 0, time.UTC).In(shanghai),
			want: time.Date(2026, 10, 15, 0, 0, 0, 0, shanghai), wantAt: "2026-10-14T16:00:00Z",
		},
		{
			name: "daily at the very first instant of the day",
			rule: capacityRule{period: Daily},
			now:  time.Date(2026, 10, 15, 0, 0, 0, 0, shanghai),
			want: time.Date(2026, 10, 15, 0, 0, 0, 0, shanghai), wantAt: "2026-10-14T16:00:00Z",
		},
		{
			name: "daily at the last instant of the day",
			rule: capacityRule{period: Daily},
			now:  time.Date(2026, 10, 15, 23, 59, 59, 999_999_999, shanghai),
			want: time.Date(2026, 10, 15, 0, 0, 0, 0, shanghai), wantAt: "2026-10-14T16:00:00Z",
		},
		{
			name: "monthly starts on the first, local midnight",
			rule: capacityRule{period: Monthly},
			now:  time.Date(2026, 10, 31, 23, 59, 59, 0, shanghai),
			want: time.Date(2026, 10, 1, 0, 0, 0, 0, shanghai), wantAt: "2026-09-30T16:00:00Z",
		},
		{
			// 20:00 UTC on the 31st is 04:00 on 1 November in Shanghai: a new
			// month there, still the old one in UTC.
			name: "monthly turns at the zone's midnight, not UTC's",
			rule: capacityRule{period: Monthly},
			now:  time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC).In(shanghai),
			want: time.Date(2026, 11, 1, 0, 0, 0, 0, shanghai), wantAt: "2026-10-31T16:00:00Z",
		},
		{
			name: "monthly across a year",
			rule: capacityRule{period: Monthly},
			now:  time.Date(2026, 1, 1, 0, 0, 0, 1, shanghai),
			want: time.Date(2026, 1, 1, 0, 0, 0, 0, shanghai), wantAt: "2025-12-31T16:00:00Z",
		},
		{
			// Clocks go back on 1 November 2026 in New York: the day has 25
			// hours, and starts at midnight EDT (04:00Z), five hours before
			// the evening's EST reading and not 24 hours before it.
			name: "daily on a 25-hour day",
			rule: capacityRule{period: Daily},
			now:  time.Date(2026, 11, 1, 23, 0, 0, 0, newYork),
			want: time.Date(2026, 11, 1, 0, 0, 0, 0, newYork), wantAt: "2026-11-01T04:00:00Z",
		},
		{
			name: "monthly spans the change of clocks",
			rule: capacityRule{period: Monthly},
			now:  time.Date(2026, 11, 20, 9, 0, 0, 0, newYork),
			want: time.Date(2026, 11, 1, 0, 0, 0, 0, newYork), wantAt: "2026-11-01T04:00:00Z",
		},
		{
			name: "total counts from its starting point",
			rule: capacityRule{period: Total, since: &reset},
			now:  time.Date(2026, 10, 15, 10, 0, 0, 0, shanghai),
			want: reset, wantAt: "2026-09-03T12:00:00Z",
		},
		{
			name: "total never recalculated counts everything",
			rule: capacityRule{period: Total},
			now:  time.Date(2026, 10, 15, 10, 0, 0, 0, shanghai),
		},
		{
			// The starting point is a total cap's; the other periods ignore it.
			name: "a starting point does not move a daily window",
			rule: capacityRule{period: Daily, since: &reset},
			now:  time.Date(2026, 10, 15, 10, 0, 0, 0, shanghai),
			want: time.Date(2026, 10, 15, 0, 0, 0, 0, shanghai), wantAt: "2026-10-14T16:00:00Z",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.rule.windowStart(c.now)
			if c.want.IsZero() {
				if got != nil {
					t.Fatalf("window starts at %v, want all history", got)
				}
				return
			}
			if got == nil || !got.Equal(c.want) {
				t.Fatalf("window starts at %v, want %v", got, c.want)
			}
			if utc := got.UTC().Format(time.RFC3339); utc != c.wantAt {
				t.Fatalf("window starts at %s UTC, want %s", utc, c.wantAt)
			}
		})
	}
}

func TestCapacityAppliesTo(t *testing.T) {
	exempt, strict := capacityRule{exempt: true}, capacityRule{exempt: false}
	for _, c := range []struct {
		rule  capacityRule
		admin bool
		want  bool
	}{
		{exempt, false, true},
		{exempt, true, false},
		{strict, false, true},
		{strict, true, true},
	} {
		if got := c.rule.appliesTo(c.admin); got != c.want {
			t.Errorf("exempt=%v admin=%v: applies = %v, want %v", c.rule.exempt, c.admin, got, c.want)
		}
	}
}

func fl(v float64) *float64 { return &v }

func period(p CapacityPeriod) *CapacityPeriod { return &p }

func TestCapacityParams(t *testing.T) {
	t.Run("no cap writes the defaults back", func(t *testing.T) {
		// A period and an exemption left over from a cap that was cleared would
		// be state nobody can see.
		no := false
		limit, p, exempt, err := capacityParams(ModelOverride{CapacityPeriod: period(Daily), CapacityAdminExempt: &no})
		if err != nil || limit.Valid || p != "total" || !exempt {
			t.Fatalf("limit=%v period=%q exempt=%v err=%v", limit, p, exempt, err)
		}
	})
	t.Run("defaults of a cap are total and exempt", func(t *testing.T) {
		limit, p, exempt, err := capacityParams(ModelOverride{Capacity: fl(20)})
		if err != nil || !limit.Valid || p != "total" || !exempt {
			t.Fatalf("limit=%v period=%q exempt=%v err=%v", limit, p, exempt, err)
		}
	})
	t.Run("a cap is kept as exact decimal", func(t *testing.T) {
		// 20.1 and 0.1+0.2 are floats with tails; the cap must be 20.100000.
		for _, c := range []struct {
			in     float64
			micros int64
		}{{20.1, 20_100_000}, {0.3, 300_000}, {0.000001, 1}, {12.345678, 12_345_678}, {1e9, 1_000_000_000_000_000}} {
			limit, _, _, err := capacityParams(ModelOverride{Capacity: fl(c.in)})
			if err != nil {
				t.Fatalf("%v: %v", c.in, err)
			}
			if limit.Exp != -6 || limit.Int.Cmp(big.NewInt(c.micros)) != 0 {
				t.Errorf("%v stored as %v e%d, want %d e-6", c.in, limit.Int, limit.Exp, c.micros)
			}
		}
	})
	t.Run("period and exemption are read", func(t *testing.T) {
		no := false
		_, p, exempt, err := capacityParams(ModelOverride{Capacity: fl(1), CapacityPeriod: period(Monthly), CapacityAdminExempt: &no})
		if err != nil || p != "monthly" || exempt {
			t.Fatalf("period=%q exempt=%v err=%v", p, exempt, err)
		}
	})
	for name, body := range map[string]ModelOverride{
		"zero":           {Capacity: fl(0)},
		"negative":       {Capacity: fl(-1)},
		"too small":      {Capacity: fl(0.0000001)},
		"seven decimals": {Capacity: fl(1.2345678)},
		"too large":      {Capacity: fl(2e9)},
		"unknown period": {Capacity: fl(1), CapacityPeriod: period("yearly")},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			_, _, _, err := capacityParams(body)
			var apiErr *httpx.APIError
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
				t.Fatalf("err = %v, want a 400", err)
			}
		})
	}
}

func TestCapacityOf(t *testing.T) {
	// A row from before the migration: no capacity, so no cap, whatever else
	// the columns hold.
	if _, ok := capacityOf(rowWith(pgtype.Numeric{}, "total", true)); ok {
		t.Error("a row with no capacity has a cap")
	}
	rule, ok := capacityOf(rowWith(pgtype.Numeric{Int: big.NewInt(5), Valid: true}, "daily", false))
	if !ok || rule.period != Daily || rule.exempt {
		t.Errorf("rule = %+v, %v", rule, ok)
	}
}

func rowWith(limit pgtype.Numeric, period string, exempt bool) sqlc.ModelOverride {
	return sqlc.ModelOverride{Capacity: limit, CapacityPeriod: period, CapacityAdminExempt: exempt}
}
