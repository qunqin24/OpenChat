package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A request's fields reach every line logged with its context, including ones
// added after the line's logger was made, and a line's own value wins.
func TestContextFields(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(contextHandler{slog.NewJSONHandler(&buf, nil)})
	ctx := WithFields(context.Background(), slog.String("request_id", "r1"))
	Annotate(ctx, slog.String("user_id", "u1"))
	log.InfoContext(ctx, "one")
	log.InfoContext(ctx, "two", "user_id", "own")
	log.Info("three")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var got []map[string]any
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		got = append(got, m)
	}
	if got[0]["request_id"] != "r1" || got[0]["user_id"] != "u1" {
		t.Errorf("line one: %v", got[0])
	}
	if got[1]["user_id"] != "own" {
		t.Errorf("a line's own value lost: %v", got[1])
	}
	if _, ok := got[2]["request_id"]; ok {
		t.Errorf("a line without the context got its fields: %v", got[2])
	}
}

// The file starts over past its size, keeping the given number of earlier ones.
func TestRotatingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	f, err := openRotating(path, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(strings.Repeat("x", 59) + "\n")
	for range 7 {
		if _, err := f.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{path, path + ".1", path + ".2"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Size() > 100 {
			t.Errorf("%s is %d bytes, over the limit", name, info.Size())
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("kept more than two earlier files")
	}
}
