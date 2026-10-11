package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Requests of one browser do not disturb each other: what a request stages -
// its files and its memories - is its own, whatever ids the browser chose.

// startGuestRun posts a request and returns once its run has begun, leaving the
// stream open. The returned function hangs up.
func (h *guestHarness) startGuestRun(body any) (hangUp func()) {
	h.t.Helper()
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, h.http.URL+"/api/v1/guest/runs", bytes.NewReader(raw))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		h.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		h.t.Fatalf("the first request was refused: %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			cancel()
			h.t.Fatal(err)
		}
		if strings.Contains(line, "run_started") {
			break
		}
	}
	return func() {
		cancel()
		resp.Body.Close()
	}
}

// A request that repeats another's ids (two tabs, a double submit) is refused,
// and cleaning up after it must not take the other's file away.
func TestGuestDuplicateRequestKeepsTheAcceptedFiles(t *testing.T) {
	h := newGuestHarness(t)
	ctx := context.Background()
	h.pace = 30 * time.Second // the first request's run stays open

	conv, attachment := uuid.New(), uuid.New()
	body := func() map[string]any {
		return map[string]any{
			"action": "send", "conversationId": conv, "model": h.model, "titled": true, "messages": []any{},
			"input": map[string]any{
				"id": uuid.MustParse("00000000-0000-4000-8000-000000000001"), "role": "user", "content": "What is this?",
				"createdAt": time.Now().Format(time.RFC3339Nano), "parts": []any{},
				"attachments": []any{map[string]any{
					"id": attachment, "filename": "dot.png", "contentType": "image/png", "size": len(testPNG),
					"data": base64.StdEncoding.EncodeToString(testPNG),
				}},
			},
		}
	}
	hangUp := h.startGuestRun(body())
	defer hangUp()

	second := h.run(body())
	if second.status != http.StatusConflict || second.error["code"] != "conversation_busy" {
		t.Fatalf("the duplicate: %d %v", second.status, second.error)
	}

	// The accepted request's attachment still points at a file that is there.
	var key string
	if err := h.pool.QueryRow(ctx, `SELECT storage_key FROM attachments WHERE id = $1`, attachment).Scan(&key); err != nil {
		t.Fatalf("the accepted request's attachment row: %v", err)
	}
	if !strings.HasPrefix(key, "uploads/guest/") || strings.Contains(key, attachment.String()) {
		t.Errorf("key %q: want a server-made name under uploads/guest/", key)
	}
	data, err := h.blobs.Read(ctx, key, 1<<20)
	if err != nil || !bytes.Equal(data, testPNG) {
		t.Fatalf("the accepted request's file is gone or changed: %v", err)
	}
	// And the refused request left nothing of its own.
	h.blobs.mu.Lock()
	files := len(h.blobs.blobs)
	h.blobs.mu.Unlock()
	if files != 1 {
		t.Errorf("%d files in storage, want the accepted request's one", files)
	}

	// Ending the accepted request removes its file, as before.
	hangUp()
	h.noGuestRows(conv)
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.blobs.mu.Lock()
		files = len(h.blobs.blobs)
		h.blobs.mu.Unlock()
		if files == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if files != 0 {
		t.Errorf("%d files left after the request ended", files)
	}
}

var memoryIDField = regexp.MustCompile(`"memoryId":"([0-9a-f-]{36})"`)

// memoryIDsIn is every memoryId in the frames' text, as the browser would read.
func memoryIDsIn(t *testing.T, g guestFrames) []string {
	t.Helper()
	raw, err := json.Marshal(g.frames)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range memoryIDField.FindAllStringSubmatch(string(raw), -1) {
		out = append(out, m[1])
	}
	return out
}

func idsOf(t *testing.T, list any) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range list.([]any) {
		m := m.(map[string]any)
		out[m["id"].(string)] = m["content"].(string)
	}
	return out
}

// Two conversations of one browser, with the same memories, run at once.
func TestGuestConcurrentConversationsShareMemories(t *testing.T) {
	h := newGuestHarness(t)
	h.pace = 300 * time.Millisecond
	memories := []any{}
	want := map[string]string{}
	for _, text := range []string{"Ada likes tea.", "Ada lives in Turin."} {
		id := uuid.NewString()
		memories = append(memories, map[string]any{"id": id, "content": text})
		want[id] = text
	}

	results := make([]guestFrames, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, _ := json.Marshal(map[string]any{
				"action": "send", "conversationId": uuid.New(), "model": h.model, "titled": true, "messages": []any{},
				"input": userRow(uuid.New(), "hi", time.Now()), "memoryEnabled": true, "memories": memories,
			})
			resp, err := http.Post(h.http.URL+"/api/v1/guest/runs", "application/json", bytes.NewReader(raw))
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			results[i].status = resp.StatusCode
			if resp.StatusCode != http.StatusOK {
				_ = json.NewDecoder(resp.Body).Decode(&results[i].error)
				return
			}
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 1<<20), 64<<20)
			for scanner.Scan() {
				var frame map[string]any
				if line := scanner.Text(); strings.HasPrefix(line, "data: ") && json.Unmarshal([]byte(line[6:]), &frame) == nil {
					results[i].frames = append(results[i].frames, frame)
				}
			}
		}()
	}
	wg.Wait()

	for i, got := range results {
		if got.status != http.StatusOK {
			t.Fatalf("conversation %d: %d %v", i, got.status, got.error)
		}
		end := got.end()
		if end == nil {
			t.Fatalf("conversation %d has no guest_end", i)
		}
		if have := idsOf(t, end["memories"]); len(have) != len(want) {
			t.Errorf("conversation %d memories %v, want %v", i, have, want)
		} else {
			for id, text := range want {
				if have[id] != text {
					t.Errorf("conversation %d memory %s = %q, want %q", i, id, have[id], text)
				}
			}
		}
		// The snapshot the server took names them by the browser's ids too.
		snapshot := end["memorySnapshot"].(map[string]any)
		inSnapshot := map[string]string{}
		for _, m := range snapshot["memories"].([]any) {
			m := m.(map[string]any)
			inSnapshot[m["id"].(string)] = m["content"].(string)
		}
		for id, text := range want {
			if inSnapshot[id] != text {
				t.Errorf("conversation %d snapshot lacks %s: %v", i, id, snapshot)
			}
		}
	}
}

// The model adds, rewrites and deletes memories across two requests of one
// conversation: the browser's ids go in, the same ids come out, and the ones
// the model made are ids the browser can send back.
func TestGuestMemoryIDsRoundTrip(t *testing.T) {
	h := newGuestHarness(t)
	conv := uuid.New()
	tea, turin := uuid.NewString(), uuid.NewString()
	browser := []any{
		map[string]any{"id": tea, "content": "Ada likes tea."},
		map[string]any{"id": turin, "content": "Ada lives in Turin."},
	}

	// First request: the model adds a note and rewrites the first.
	h.script = func(n int, _ string) string {
		switch n {
		case 1:
			return openAIToolCall("call_a", "memory", `{"op":"add","content":"Ada plays chess."}`)
		case 2:
			return openAIToolCall("call_b", "memory", `{"op":"update","id":"m1","content":"Ada likes green tea."}`)
		}
		return openAIWire.text("Noted.")
	}
	first := h.run(map[string]any{
		"action": "send", "conversationId": conv, "model": h.model, "titled": true, "messages": []any{},
		"input": userRow(uuid.New(), "remember", time.Now()), "memoryEnabled": true, "memories": browser,
	})
	end := first.end()
	if end == nil {
		t.Fatalf("no end: %d %v", first.status, first.error)
	}
	have := idsOf(t, end["memories"])
	if len(have) != 3 || have[tea] != "Ada likes green tea." || have[turin] != "Ada lives in Turin." {
		t.Fatalf("memories after the run: %v", have)
	}
	var chess string
	for id, text := range have {
		if text == "Ada plays chess." {
			chess = id
		}
	}
	if _, err := uuid.Parse(chess); err != nil {
		t.Fatalf("the model's new memory has no usable id: %v", have)
	}
	// The log names the memories by the browser's ids.
	seen := map[string]string{}
	for _, e := range end["memoryEvents"].([]any) {
		e := e.(map[string]any)
		seen[e["memoryId"].(string)] = e["op"].(string)
	}
	if len(seen) != 2 || seen[chess] != "add" || seen[tea] != "update" {
		t.Errorf("memory events by memory id: %v", seen)
	}
	// What the tool reported along the way, and in the last frame, is the same.
	allowed := map[string]bool{tea: true, turin: true, chess: true}
	for _, id := range memoryIDsIn(t, first) {
		if !allowed[id] {
			t.Errorf("a staged id left the server: %s", id)
		}
	}
	snapshot := end["memorySnapshot"].(map[string]any)
	for _, m := range snapshot["memories"].([]any) {
		if id := m.(map[string]any)["id"].(string); id != tea && id != turin {
			t.Errorf("snapshot memory %s is not one the browser sent", id)
		}
	}
	added := snapshot["added"].([]any)
	if len(added) != 1 || added[0].(map[string]any)["id"] != chess {
		t.Fatalf("snapshot.added: %v", added)
	}
	chessRef := added[0].(map[string]any)["ref"].(string)

	// Second request, as the browser would send it: its own list and snapshot.
	h.mu.Lock()
	h.bodies = nil
	h.mu.Unlock()
	h.script = func(n int, _ string) string {
		switch n {
		case 1:
			return openAIToolCall("call_c", "memory", `{"op":"update","id":"`+chessRef+`","content":"Ada plays blitz chess."}`)
		case 2:
			return openAIToolCall("call_d", "memory", `{"op":"delete","id":"m2"}`)
		}
		return openAIWire.text("Done.")
	}
	var list []any
	for id, text := range have {
		list = append(list, map[string]any{"id": id, "content": text})
	}
	second := h.run(map[string]any{
		"action": "send", "conversationId": conv, "model": h.model, "titled": true,
		"messages":       []any{userRow(uuid.New(), "remember", time.Now().Add(-time.Hour))},
		"input":          userRow(uuid.New(), "more", time.Now()),
		"memoryEnabled":  true,
		"memories":       list,
		"memorySnapshot": end["memorySnapshot"],
	})
	end = second.end()
	if end == nil {
		t.Fatalf("no end: %d %v", second.status, second.error)
	}
	have = idsOf(t, end["memories"])
	if len(have) != 2 || have[chess] != "Ada plays blitz chess." || have[tea] != "Ada likes green tea." {
		t.Fatalf("memories after the second run: %v", have)
	}
	seen = map[string]string{}
	for _, e := range end["memoryEvents"].([]any) {
		e := e.(map[string]any)
		seen[e["memoryId"].(string)] = e["op"].(string)
	}
	if seen[chess] != "update" || seen[turin] != "delete" {
		t.Errorf("memory events by memory id: %v", seen)
	}
	for _, id := range memoryIDsIn(t, second) {
		if !allowed[id] {
			t.Errorf("a staged id left the server: %s", id)
		}
	}
}
