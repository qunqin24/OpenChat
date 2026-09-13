package sse

import (
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func collect(t *testing.T, scanner *Scanner) ([]Event, error) {
	t.Helper()
	var events []Event
	for scanner.Scan() {
		events = append(events, scanner.Event())
	}
	return events, scanner.Err()
}

func TestScannerFramesRealWorldStreams(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Event
	}{
		{
			name:  "one frame",
			input: "data: hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "CRLF line endings",
			input: "data: hello\r\n\r\ndata: again\r\n\r\n",
			want:  []Event{{Data: "hello"}, {Data: "again"}},
		},
		{
			name: "comments are heartbeats, not frames",
			// An endpoint that sends nothing for a minute and an endpoint that
			// has died look identical without these.
			input: ": keep-alive\n\ndata: hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name: "multi-line data joins with newlines",
			// Exactly one space after the colon is the separator; anything
			// further in is part of the payload.
			input: "data: {\ndata:   \"a\": 1\ndata: }\n\n",
			want:  []Event{{Data: "{\n  \"a\": 1\n}"}},
		},
		{
			name:  "a named event",
			input: "event: ping\ndata: {}\n\n",
			want:  []Event{{Name: "ping", Data: "{}"}},
		},
		{
			name:  "padding between frames is not a frame",
			input: "\n\n\ndata: hello\n\n\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name: "a final frame missing its blank line still counts",
			// Every line arrived intact; only the separator is missing, which
			// is common enough in the wild to accept.
			input: "data: hello\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "fields we do not use are ignored",
			input: "id: 42\nretry: 1000\ndata: hello\n\n",
			want:  []Event{{Data: "hello"}},
		},
		{
			name:  "an empty stream has no frames",
			input: "",
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collect(t, NewScanner(strings.NewReader(tc.input)))
			if err != nil {
				t.Fatalf("Err: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("events = %#v, want %#v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("event %d = %#v, want %#v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestScannerReportsAStreamThatDiedMidLine(t *testing.T) {
	// Dispatching the half-written frame would turn a cut connection into a
	// short answer, which is the one failure mode nobody can see afterwards.
	scanner := NewScanner(strings.NewReader("data: complete\n\ndata: half a fra"))

	got, err := collect(t, scanner)
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Err = %v, want ErrTruncated", err)
	}
	if len(got) != 1 || got[0].Data != "complete" {
		t.Errorf("events = %#v, want the complete frame only", got)
	}
}

func TestScannerRefusesAnUnboundedEvent(t *testing.T) {
	// An endpoint that never sends a blank line would otherwise buffer until
	// the process dies.
	scanner := NewScanner(strings.NewReader("data: " + strings.Repeat("x", 100) + "\n\n"))
	scanner.SetMaxEventBytes(32)

	if _, err := collect(t, scanner); !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("Err = %v, want ErrEventTooLarge", err)
	}
}

func TestScannerHandlesFramesSplitAcrossReads(t *testing.T) {
	// One byte per read is the pathological version of what the network does
	// to every stream.
	input := "data: first\n\ndata: second\n\n"
	scanner := NewScanner(iotest.OneByteReader(strings.NewReader(input)))

	got, err := collect(t, scanner)
	if err != nil {
		t.Fatalf("Err: %v", err)
	}
	if len(got) != 2 || got[0].Data != "first" || got[1].Data != "second" {
		t.Errorf("events = %#v", got)
	}
}

func TestScannerStopsAtTheFirstReadError(t *testing.T) {
	broken := errors.New("connection reset")
	scanner := NewScanner(iotest.ErrReader(broken))

	if _, err := collect(t, scanner); !errors.Is(err, broken) {
		t.Fatalf("Err = %v, want the reader's error", err)
	}
}
