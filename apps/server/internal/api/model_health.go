package api

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// The model marketplace's recent tab: how each model has done in the last six
// hours, from the usage ledger, in 15-minute buckets for its trend. Every text
// model on offer is listed, those with requests first (busiest first), then
// the rest by name with nothing to show; a model with requests that is no
// longer offered stays, among the first.

const (
	healthBucket  = 15 * time.Minute
	healthBuckets = 24
)

// GetModelHealth answers the recent tab.
func (s *Server) GetModelHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The current bucket is the last; the window starts 23 before it. Go's
	// Truncate counts from year 1, which is a whole number of 15 minutes
	// before the epoch, so these are the buckets date_bin makes.
	last := time.Now().UTC().Truncate(healthBucket)
	since := last.Add(-(healthBuckets - 1) * healthBucket)
	at := pgtype.Timestamptz{Time: since, Valid: true}

	rows, err := s.q.GetModelHealth(ctx, at)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	points, err := s.q.GetModelHealthBuckets(ctx, at)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ModelID)
	}
	// Names and marks whether or not a channel serves the model now: a model
	// removed in the last six hours still has its requests here.
	marks := map[string]ModelMark{}
	if len(ids) > 0 {
		found, err := s.q.ModelMarks(ctx, ids)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		for _, m := range found {
			marks[m.ModelID] = ModelMark{Id: m.ModelID, Name: m.Name, ProviderId: m.ProviderID, Family: m.Family}
		}
	}

	buckets := make([]time.Time, healthBuckets)
	for i := range buckets {
		buckets[i] = since.Add(time.Duration(i) * healthBucket)
	}
	series := make(map[string][]ModelHealthPoint, len(rows))
	for _, row := range rows {
		series[row.ModelID] = make([]ModelHealthPoint, healthBuckets)
	}
	for _, p := range points {
		line, ok := series[p.ModelID]
		i := int(p.At.Time.Sub(since) / healthBucket)
		if !ok || i < 0 || i >= healthBuckets {
			continue
		}
		line[i] = ModelHealthPoint{Requests: p.Requests, Errors: p.Errors, FirstTokenP50Ms: measured(p.FirstTokenP50Ms), SpeedP50: measured(p.SpeedP50)}
	}

	models := make([]ModelHealth, 0, len(rows))
	for _, row := range rows {
		mark, ok := marks[row.ModelID]
		if !ok {
			mark = ModelMark{Name: row.ModelID}
		}
		models = append(models, ModelHealth{
			ModelId:         row.ModelID,
			Name:            mark.Name,
			ProviderId:      mark.ProviderId,
			Family:          mark.Family,
			Requests:        row.Requests,
			Errors:          row.Errors,
			InputTokens:     row.InputTokens,
			OutputTokens:    row.OutputTokens,
			Cost:            row.Cost,
			FirstTokenP50Ms: measured(row.FirstTokenP50Ms),
			FirstTokenP95Ms: measured(row.FirstTokenP95Ms),
			SpeedP50:        measured(row.SpeedP50),
			DurationP50Ms:   measured(row.DurationP50Ms),
			Series:          series[row.ModelID],
		})
	}
	// The models on offer that had nothing to do, after the busy ones.
	offered, err := s.chatModels(ctx, asker{staff: true, admin: true, ignoreCap: true})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	idle := make([]ModelHealth, 0, len(offered))
	for _, m := range offered {
		if _, busy := series[m.Id]; busy {
			continue
		}
		idle = append(idle, ModelHealth{
			ModelId: m.Id, Name: m.Name, ProviderId: m.ProviderId, Family: m.Family,
			Series: make([]ModelHealthPoint, healthBuckets),
		})
	}
	slices.SortStableFunc(idle, func(a, b ModelHealth) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	models = append(models, idle...)

	httpx.JSON(w, http.StatusOK, ModelHealthReport{Buckets: buckets, BucketMinutes: int(healthBucket / time.Minute), Models: models})
}

// measured is a figure the query reports as -1 when there was nothing to
// measure, as absent.
func measured(v float64) *float64 {
	if v < 0 {
		return nil
	}
	return &v
}
