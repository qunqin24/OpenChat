package api

import (
	"math/rand/v2"
	"testing"

	"github.com/google/uuid"

	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
)

func TestServingOrderPutsHigherPriorityFirst(t *testing.T) {
	high, low1, low2 := uuid.New(), uuid.New(), uuid.New()
	rows := []sqlc.ServingChannelsRow{
		{ID: high, Priority: 10, Weight: 1},
		{ID: low1, Priority: 0, Weight: 1},
		{ID: low2, Priority: 0, Weight: 1},
	}
	r := rand.New(rand.NewPCG(1, 2))
	for range 50 {
		order := servingOrder(rows, r.Float64)
		if len(order) != 3 || order[0] != high {
			t.Fatalf("order %v: the higher priority must come first, every channel once", order)
		}
	}
}

func TestServingOrderDrawsWithinAPriorityByWeight(t *testing.T) {
	heavy, light := uuid.New(), uuid.New()
	rows := []sqlc.ServingChannelsRow{
		{ID: light, Priority: 0, Weight: 1},
		{ID: heavy, Priority: 0, Weight: 3},
	}
	r := rand.New(rand.NewPCG(3, 4))
	first := 0
	const draws = 4000
	for range draws {
		order := servingOrder(rows, r.Float64)
		if len(order) != 2 || order[0] == order[1] {
			t.Fatalf("order %v", order)
		}
		if order[0] == heavy {
			first++
		}
	}
	// Weight 3 beside weight 1: first about three times in four.
	if share := float64(first) / draws; share < 0.70 || share > 0.80 {
		t.Fatalf("the weight-3 channel went first in %.2f of draws, want about 0.75", share)
	}
}

func TestEndpointForCompletesAHostOnlyAddress(t *testing.T) {
	cases := []struct {
		kind ChannelType
		in   string
		want string
	}{
		// Blank: the type's default.
		{"openai", "", "https://api.openai.com/v1"},
		// A host alone, with or without the trailing slash: the type's path.
		{"openai-compatible", "https://relay.example", "https://relay.example/v1"},
		{"openai-compatible", "https://relay.example/", "https://relay.example/v1"},
		{"dashscope", "https://dashscope.example", "https://dashscope.example/compatible-mode/v1"},
		{"gemini", "https://gemini-proxy.example/", "https://gemini-proxy.example/v1beta"},
		{"anthropic", "https://claude-proxy.example", "https://claude-proxy.example/v1"},
		// Any path is the operator's to choose.
		{"openai-compatible", "https://relay.example/v1", "https://relay.example/v1"},
		{"openai-compatible", "https://relay.example/v1/", "https://relay.example/v1/"},
		{"openai-compatible", "https://relay.example/api/openai", "https://relay.example/api/openai"},
		// A relay with nothing given has no default.
		{"openai-compatible", "  ", ""},
	}
	for _, c := range cases {
		if got := endpointFor(c.kind, c.in); got != c.want {
			t.Errorf("endpointFor(%s, %q) = %q, want %q", c.kind, c.in, got, c.want)
		}
	}
}
