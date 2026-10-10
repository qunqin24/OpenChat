package agent

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// maxPendingBatches bounds what a slow subscriber can hold. Past it, the
// pending batches are thrown away and the subscriber is told to take a fresh
// snapshot instead — it converges on the current state without being promised
// every step on the way (pi-durable §9.4).
const maxPendingBatches = 100

// Hub fans committed batches out to a conversation's subscribers.
//
// In-process only. One server process owns the runs it executes, and a
// subscriber connected to another process would see nothing until it
// reconnected; spreading the API over several processes needs this replaced by
// a broker (Postgres LISTEN/NOTIFY would do) and runs assigned by lease.
type Hub struct {
	mu     sync.Mutex
	topics map[uuid.UUID]map[*Subscription]struct{}
}

func NewHub() *Hub {
	return &Hub{topics: map[uuid.UUID]map[*Subscription]struct{}{}}
}

// Subscription receives one conversation's batches.
type Subscription struct {
	hub            *Hub
	conversationID uuid.UUID

	mu      sync.Mutex
	pending []Batch
	resync  bool
	notify  chan struct{}
}

// Subscribe registers before the caller reads its snapshot, so nothing
// committed in between is missed: a batch the snapshot already contains is
// recognised by its version and dropped.
func (h *Hub) Subscribe(conversationID uuid.UUID) *Subscription {
	sub := &Subscription{hub: h, conversationID: conversationID, notify: make(chan struct{}, 1)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.topics[conversationID] == nil {
		h.topics[conversationID] = map[*Subscription]struct{}{}
	}
	h.topics[conversationID][sub] = struct{}{}
	return sub
}

func (s *Subscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	subs := s.hub.topics[s.conversationID]
	delete(subs, s)
	if len(subs) == 0 {
		delete(s.hub.topics, s.conversationID)
	}
}

// Publish delivers a batch to every subscriber of the conversation. It never
// blocks on a subscriber.
func (h *Hub) Publish(conversationID uuid.UUID, batch Batch) {
	h.mu.Lock()
	subs := make([]*Subscription, 0, len(h.topics[conversationID]))
	for sub := range h.topics[conversationID] {
		subs = append(subs, sub)
	}
	h.mu.Unlock()

	for _, sub := range subs {
		sub.mu.Lock()
		if !sub.resync {
			if len(sub.pending) >= maxPendingBatches {
				sub.pending = nil
				sub.resync = true
			} else {
				sub.pending = append(sub.pending, batch)
			}
		}
		sub.mu.Unlock()
		select {
		case sub.notify <- struct{}{}:
		default:
		}
	}
}

// Next waits for batches. resync means the subscriber fell too far behind:
// the batches it missed are gone and it must start again from a snapshot.
func (s *Subscription) Next(ctx context.Context) (batches []Batch, resync bool, err error) {
	for {
		s.mu.Lock()
		if s.resync || len(s.pending) > 0 {
			batches, resync = s.pending, s.resync
			s.pending, s.resync = nil, false
			s.mu.Unlock()
			return batches, resync, nil
		}
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-s.notify:
		}
	}
}

// Drain returns what is pending without waiting, for a caller selecting on
// Wake alongside its own timers.
func (s *Subscription) Drain() (batches []Batch, resync bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	batches, resync = s.pending, s.resync
	s.pending, s.resync = nil, false
	return batches, resync
}

// Wake returns a channel that receives when Next has something to return,
// for a caller that also waits on a timer.
func (s *Subscription) Wake() <-chan struct{} { return s.notify }
