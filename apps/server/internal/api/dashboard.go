package api

import (
	"net/http"

	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// dashboardTop is how many rows each ranked list of the dashboard keeps.
const dashboardTop = 8

// GetAdminDashboard reports how the model requests of a window went, from the
// usage ledger, next to the equal-length window before it.
func (s *Server) GetAdminDashboard(w http.ResponseWriter, r *http.Request, params GetAdminDashboardParams) {
	rng := DashboardRange("7d")
	if params.Range != nil {
		rng = *params.Range
	}
	var bucket string
	var buckets int32
	switch string(rng) {
	case "24h":
		bucket, buckets = "hour", 24
	case "7d":
		bucket, buckets = "day", 7
	case "30d":
		bucket, buckets = "day", 30
	case "90d":
		bucket, buckets = "day", 90
	default:
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_range", "unknown range: "+string(rng)))
		return
	}

	ctx := r.Context()
	win, err := s.q.GetDashboardWindow(ctx, sqlc.GetDashboardWindowParams{Bucket: bucket, Buckets: buckets})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	since := win.Since

	// Seven scans of the same index range, one after the other: the dashboard
	// is read now and then by an operator, not worth seven pool connections.
	var (
		current, previous sqlc.GetDashboardKpisRow
		trend             []sqlc.GetDashboardTrendRow
		models            []sqlc.GetDashboardModelsRow
		channels          []sqlc.GetDashboardChannelsRow
		users             []sqlc.GetDashboardUsersRow
		errs              []sqlc.GetDashboardErrorsRow
	)
	steps := []func() error{
		func() (err error) {
			current, err = s.q.GetDashboardKpis(ctx, sqlc.GetDashboardKpisParams{FromTime: since})
			return
		},
		func() (err error) {
			previous, err = s.q.GetDashboardKpis(ctx, sqlc.GetDashboardKpisParams{FromTime: win.PrevSince, ToTime: since})
			return
		},
		func() (err error) {
			trend, err = s.q.GetDashboardTrend(ctx, sqlc.GetDashboardTrendParams{Bucket: bucket, Since: since})
			return
		},
		func() (err error) {
			models, err = s.q.GetDashboardModels(ctx, sqlc.GetDashboardModelsParams{Since: since, RowLimit: dashboardTop})
			return
		},
		func() (err error) {
			channels, err = s.q.GetDashboardChannels(ctx, sqlc.GetDashboardChannelsParams{Since: since, RowLimit: dashboardTop})
			return
		},
		func() (err error) {
			users, err = s.q.GetDashboardUsers(ctx, sqlc.GetDashboardUsersParams{Since: since, RowLimit: dashboardTop})
			return
		},
		func() (err error) {
			errs, err = s.q.GetDashboardErrors(ctx, sqlc.GetDashboardErrorsParams{Since: since, RowLimit: dashboardTop})
			return
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}

	out := AdminDashboard{
		Range:      rng,
		Since:      since.Time,
		Bucket:     AdminDashboardBucket(bucket),
		TotalUsers: win.TotalUsers,
		Current:    toDashboardKpis(current),
		Previous:   toDashboardKpis(previous),
		Trend:      make([]DashboardTrendPoint, 0, len(trend)),
		Models:     make([]DashboardModel, 0, len(models)),
		Channels:   make([]DashboardChannel, 0, len(channels)),
		Users:      make([]DashboardUser, 0, len(users)),
		Errors:     make([]DashboardError, 0, len(errs)),
	}
	for _, p := range trend {
		out.Trend = append(out.Trend, DashboardTrendPoint{At: p.At.Time, Requests: p.Requests, Errors: p.Errors, Cost: p.Cost})
	}
	for _, m := range models {
		out.Models = append(out.Models, DashboardModel{
			Model: m.ModelID, Requests: m.Requests, Errors: m.Errors,
			InputTokens: m.InputTokens, OutputTokens: m.OutputTokens, CacheReadTokens: m.CacheReadTokens,
			CacheHitRate: hitRate(m.CacheReadTokens, m.CachePromptTokens), Cost: m.Cost,
			FirstTokenP50Ms: latency(m.FirstTokenP50Ms),
		})
	}
	for _, c := range channels {
		ch := DashboardChannel{
			ChannelId: c.ChannelID, ChannelName: c.ChannelName, Requests: c.Requests, Errors: c.Errors,
			FirstTokenP50Ms: latency(c.FirstTokenP50Ms), FirstTokenP95Ms: latency(c.FirstTokenP95Ms),
			DurationP50Ms: latency(c.DurationP50Ms), DurationP95Ms: latency(c.DurationP95Ms),
		}
		if c.LastErrorAt.Valid {
			at := c.LastErrorAt.Time
			ch.LastErrorAt = &at
		}
		out.Channels = append(out.Channels, ch)
	}
	for _, u := range users {
		out.Users = append(out.Users, DashboardUser{
			UserId: u.UserID, UserName: u.UserName, UserEmail: u.UserEmail, UserAvatarUrl: s.avatarURL(ctx, u.UserAvatarKey),
			DeletedUser: nonEmpty(u.DeletedUser),
			Requests:    u.Requests, Cost: u.Cost,
		})
	}
	for _, e := range errs {
		out.Errors = append(out.Errors, DashboardError{
			Id: e.ID, CreatedAt: e.CreatedAt.Time, Model: e.ModelID, ChannelName: e.ChannelName,
			ErrorCode: e.ErrorCode, ErrorMessage: e.ErrorMessage,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func toDashboardKpis(k sqlc.GetDashboardKpisRow) DashboardKpis {
	return DashboardKpis{
		Requests: k.Requests, Errors: k.Errors,
		InputTokens: k.InputTokens, OutputTokens: k.OutputTokens,
		CacheReadTokens: k.CacheReadTokens, CacheWriteTokens: k.CacheWriteTokens,
		CacheHitRate: hitRate(k.CacheReadTokens, k.CachePromptTokens), CacheSavings: savings(k.CacheSavings),
		Cost: k.Cost, ActiveUsers: k.ActiveUsers,
		FirstTokenP50Ms: latency(k.FirstTokenP50Ms), FirstTokenP95Ms: latency(k.FirstTokenP95Ms),
	}
}

// hitRate is the share of prompt tokens served from the cache: read over
// everything sent (input, which excludes the cached part, plus read and write).
// Nil when nothing was sent -- no rate, rather than 0%.
func hitRate(cacheRead, prompt int64) *float64 {
	if prompt <= 0 {
		return nil
	}
	rate := float64(cacheRead) / float64(prompt)
	return &rate
}

// savings is nil where no request was priced for both input and cache reads.
func savings(usd float64) *float64 {
	if usd <= 0 {
		return nil
	}
	return &usd
}

// latency reads a percentile the queries report as -1 when there was nothing
// to take it of.
func latency(ms float64) *float64 {
	if ms < 0 {
		return nil
	}
	return &ms
}

// nonEmpty is s, or nil when there is none.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
