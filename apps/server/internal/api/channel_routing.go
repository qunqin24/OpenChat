package api

import (
	"context"
	"math/rand/v2"
	"slices"

	"github.com/google/uuid"

	"github.com/qunqin24/openchat/apps/server/internal/agent"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
)

// servingOrder is the order a run of a model tries the channels that offer it:
// the highest priority first (the rows arrive sorted that way), and within one
// priority a draw weighted by each channel's weight, without replacement - so a
// channel of weight 3 beside one of weight 1 goes first about three times in
// four, and the other is next. The first is where a run starts; the rest are
// where it fails over to, in turn (agent failover). random returns [0, 1).
func servingOrder(rows []sqlc.ServingChannelsRow, random func() float64) []uuid.UUID {
	order := make([]uuid.UUID, 0, len(rows))
	for start := 0; start < len(rows); {
		end := start
		for end < len(rows) && rows[end].Priority == rows[start].Priority {
			end++
		}
		group := slices.Clone(rows[start:end])
		for len(group) > 0 {
			total := 0
			for _, row := range group {
				total += max(int(row.Weight), 1)
			}
			at := random() * float64(total)
			pick := len(group) - 1
			for i, row := range group {
				at -= float64(max(int(row.Weight), 1))
				if at < 0 {
					pick = i
					break
				}
			}
			order = append(order, group[pick].ID)
			group = slices.Delete(group, pick, pick+1)
		}
		start = end
	}
	return order
}

// NextChannel is where a run of modelID goes after the channels it has tried
// failed it: the next in a fresh serving order that it has not tried, or nil
// when none is left. Fresh rather than remembered, so a channel switched off,
// or one added, since the run started is taken into account.
func (m *ModelResolver) NextChannel(ctx context.Context, modelID string, tried []uuid.UUID) (*uuid.UUID, error) {
	rows, err := m.q.ServingChannels(ctx, modelID)
	if err != nil {
		return nil, err
	}
	for _, id := range servingOrder(rows, rand.Float64) {
		if !slices.Contains(tried, id) {
			return &id, nil
		}
	}
	return nil, nil
}

var _ agent.ChannelFailover = (*ModelResolver)(nil)
