package tools

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The ranking of the chat_history tool's search (docs/design/chat-history-plan.md,
// section 4.2), as pure functions. Candidates are messages that contain at
// least one of the keywords. Each is scored in the form of BM25; the score of
// a conversation is the sum of its candidates'; the two ranks are fused with
// reciprocal rank fusion, so that a message from a conversation that is about
// the matter counts for more than the same message by itself.

const (
	// bm25K1 and bm25B are the usual BM25 constants: how fast repeating a
	// word stops adding to the score, and how much a long message is held
	// against it.
	bm25K1 = 1.2
	bm25B  = 0.75
	// rrfK is the constant of reciprocal rank fusion.
	rrfK = 60
	// searchHits is how many matches a search returns before they are merged.
	searchHits = 5
	// contextMessages is how many messages on each side of a match are shown.
	contextMessages = 2
)

// keywordStats is what the ranking knows about the history and the keywords of
// a search.
type keywordStats struct {
	// Total is the number of searchable messages the person has, AvgLength
	// their average size in bytes.
	Total     int
	AvgLength float64
	// Hits is, per keyword, how many of the candidates contain it.
	Hits []int
}

// statsOf counts the keywords among the candidates. The count stops at what
// the query returns per keyword, so a word found everywhere is counted as found
// often rather than exactly: what the ranking needs is that the rare keyword
// weighs more than the common one, and that it still does. Reading the whole
// history once more per keyword to know the exact number would cost more than
// the search itself.
func statsOf(cands []candidate, keywords, total int, avgLength float64) keywordStats {
	stats := keywordStats{Total: total, AvgLength: avgLength, Hits: make([]int, keywords)}
	for _, c := range cands {
		for i, tf := range c.Terms {
			if tf > 0 && i < keywords {
				stats.Hits[i]++
			}
		}
	}
	return stats
}

// candidate is a message that matched.
type candidate struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	// Length is the message's size in bytes.
	Length int
	// Terms is how often each keyword occurs in it.
	Terms []int
	// Order breaks ties: a larger one is newer (later in the result of the
	// query, which is newest first, means smaller).
	Order int
}

// bm25 is the score of a message with the given term counts and length.
func bm25(terms []int, length int, stats keywordStats) float64 {
	avg := stats.AvgLength
	if avg <= 0 {
		avg = float64(max(length, 1))
	}
	norm := 1 - bm25B + bm25B*float64(length)/avg
	var score float64
	for i, tf := range terms {
		if tf <= 0 {
			continue
		}
		df := 0
		if i < len(stats.Hits) {
			df = stats.Hits[i]
		}
		n := float64(max(stats.Total, df))
		idf := math.Log(1 + (n-float64(df)+0.5)/(float64(df)+0.5))
		f := float64(tf)
		score += idf * f * (bm25K1 + 1) / (f + bm25K1*norm)
	}
	return score
}

// rankedHit is a candidate with its place in the final order.
type rankedHit struct {
	candidate
	MessageRank, ConversationRank int // from 1
	Score                         float64
	Fused                         float64
}

// rank orders the candidates, best first, and keeps the first limit.
func rank(cands []candidate, stats keywordStats, limit int) []rankedHit {
	hits := make([]rankedHit, len(cands))
	for i, c := range cands {
		hits[i] = rankedHit{candidate: c, Score: bm25(c.Terms, c.Length, stats)}
	}
	byScore := func(a, b rankedHit) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return a.Order - b.Order // the newer first
	}
	sort.SliceStable(hits, func(i, j int) bool { return byScore(hits[i], hits[j]) < 0 })
	for i := range hits {
		hits[i].MessageRank = i + 1
	}

	// A conversation's score is the sum of its candidates'; ties go to the
	// one with the newest candidate (the smallest Order).
	type conv struct {
		id     uuid.UUID
		score  float64
		newest int
	}
	byID := map[uuid.UUID]*conv{}
	var convs []*conv
	for _, h := range hits {
		c := byID[h.ConversationID]
		if c == nil {
			c = &conv{id: h.ConversationID, newest: h.Order}
			byID[h.ConversationID] = c
			convs = append(convs, c)
		}
		c.score += h.Score
		c.newest = min(c.newest, h.Order)
	}
	sort.SliceStable(convs, func(i, j int) bool {
		if convs[i].score != convs[j].score {
			return convs[i].score > convs[j].score
		}
		return convs[i].newest < convs[j].newest
	})
	convRank := make(map[uuid.UUID]int, len(convs))
	for i, c := range convs {
		convRank[c.id] = i + 1
	}

	for i := range hits {
		hits[i].ConversationRank = convRank[hits[i].ConversationID]
		hits[i].Fused = 1/float64(rrfK+hits[i].MessageRank) + 1/float64(rrfK+hits[i].ConversationRank)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Fused != hits[j].Fused {
			return hits[i].Fused > hits[j].Fused
		}
		return hits[i].MessageRank < hits[j].MessageRank
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// span is a stretch of one conversation's messages (by their place among the
// searchable ones) that shows one or more hits with their surroundings.
type span struct {
	ConversationID uuid.UUID
	// From and To are the places of the first and last message shown.
	From, To int
	// Hits are the places of the matches inside it.
	Hits []int
}

// place is where a hit stands in its conversation.
type place struct {
	ConversationID uuid.UUID
	N              int
}

// mergeSpans gives every hit the messages around it and joins those of one
// conversation whose surroundings overlap or touch. The spans come in the
// order of their best hit (hits are given best first).
func mergeSpans(hits []place, around int) []span {
	var spans []span
	for _, h := range hits {
		from, to := max(h.N-around, 1), h.N+around
		merged := false
		for i := range spans {
			s := &spans[i]
			if s.ConversationID == h.ConversationID && from <= s.To+1 && to >= s.From-1 {
				s.From, s.To = min(s.From, from), max(s.To, to)
				s.Hits = append(s.Hits, h.N)
				merged = true
				break
			}
		}
		if !merged {
			spans = append(spans, span{ConversationID: h.ConversationID, From: from, To: to, Hits: []int{h.N}})
		}
	}
	// A hit that bridged two spans leaves them overlapping; fold those.
	for changed := true; changed; {
		changed = false
	scan:
		for i := range spans {
			for j := i + 1; j < len(spans); j++ {
				a, b := &spans[i], spans[j]
				if a.ConversationID == b.ConversationID && b.From <= a.To+1 && b.To >= a.From-1 {
					a.From, a.To = min(a.From, b.From), max(a.To, b.To)
					a.Hits = append(a.Hits, b.Hits...)
					spans = append(spans[:j], spans[j+1:]...)
					changed = true
					break scan
				}
			}
		}
	}
	return spans
}

// countTerm is how often keyword occurs in text, ignoring case.
func countTerm(text, keyword string) int {
	keyword = strings.ToLower(keyword)
	if keyword == "" {
		return 0
	}
	return strings.Count(strings.ToLower(text), keyword)
}

// termCounts is countTerm for each keyword. A message the database matched but
// whose case folding differs from Go's counts as having its first keyword
// once, so that it is not lost.
func termCounts(text string, keywords []string) []int {
	counts := make([]int, len(keywords))
	matched := false
	for i, k := range keywords {
		counts[i] = countTerm(text, k)
		matched = matched || counts[i] > 0
	}
	if !matched && len(counts) > 0 {
		counts[0] = 1
	}
	return counts
}

// cutRunes shortens text to at most n characters (not bytes), ending in an
// ellipsis when it was cut.
func cutRunes(text string, n int) string {
	if n <= 0 || utf8.RuneCountInString(text) <= n {
		return text
	}
	return strings.TrimSpace(string([]rune(text)[:n-1])) + "…"
}

// excerpt shortens text to at most n characters. Without keywords, or when
// none of them is found, that is its beginning. Otherwise it is the stretch
// around the first place one of them occurs (ignoring case), a quarter of it
// before the keyword, so that a fact deep inside a long answer is what the
// search shows. An ellipsis marks each end that was cut.
func excerpt(text string, keywords []string, n int) string {
	runes := []rune(text)
	if n <= 0 || len(runes) <= n {
		return text
	}
	at := -1
	lowered := make([]rune, len(runes))
	for i, r := range runes {
		lowered[i] = unicode.ToLower(r)
	}
	for _, k := range keywords {
		if i := indexRunes(lowered, []rune(strings.ToLower(k))); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	start := 0
	if at > n/4 {
		start = min(at-n/4, len(runes)-n)
	}
	if start == 0 {
		return cutRunes(text, n)
	}
	end := min(start+n, len(runes))
	out := "…" + strings.TrimSpace(string(runes[start+1:end]))
	if end < len(runes) {
		out = strings.TrimSpace(string([]rune(out)[:n-1])) + "…"
	}
	return out
}

// indexRunes is where needle first occurs in hay, or -1.
func indexRunes(hay, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return -1
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i] == needle[0] && slices.Equal(hay[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}
