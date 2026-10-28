package doctext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
)

// A PDF's words come from poppler's pdftotext, run as a program of its own: a
// PDF is the most intricate of the formats people send, poppler reads the
// wide variety of them well (Chinese fonts, several columns), and a document
// that makes it fail or hang takes that process down, not the server. It runs
// with a time limit, a bound on what it may write, and a few at a time.
//
// Its reading order is kept (no -layout): a page's text as it is read, not
// padded into columns with spaces. Pages are separated by form feeds, and each
// is given a "[Page n]" line so a reader can say where something is.

const (
	pdfTimeout = 30 * time.Second
	// pdfOutputBytes bounds what pdftotext may write, past which the text is
	// too long anyway.
	pdfOutputBytes = MaxTextBytes + MaxTextBytes/2
	// scannedRunes is the fewest letters a page averages to be taken as text:
	// fewer, and the pages are pictures.
	scannedRunes = 30
)

var (
	pdfSlots = make(chan struct{}, 4)

	lookOnce sync.Once
	lookPath string
)

// PDFReadable is whether this server can read a PDF's text: pdftotext is
// installed. Without it a PDF goes only to models that read PDFs.
func PDFReadable() bool {
	lookOnce.Do(func() { lookPath, _ = exec.LookPath("pdftotext") })
	return lookPath != ""
}

func pdf(ctx context.Context, data []byte) (Text, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return Text{}, ErrUnreadable
	}
	if !PDFReadable() {
		return Text{}, ErrNoReader
	}
	select {
	case pdfSlots <- struct{}{}:
		defer func() { <-pdfSlots }()
	case <-ctx.Done():
		return Text{}, ctx.Err()
	}
	// pdftotext reads a file: a pipe is not seekable, and a PDF is read from
	// its end.
	file, err := os.CreateTemp("", "openchat-*.pdf")
	if err != nil {
		return Text{}, err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return Text{}, err
	}
	if err := file.Close(); err != nil {
		return Text{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, pdfTimeout)
	defer cancel()
	out := &cappedBuffer{limit: pdfOutputBytes}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, lookPath, "-enc", "UTF-8", "-eol", "unix", "-q", file.Name(), "-")
	cmd.Stdout, cmd.Stderr = out, &stderr
	cmd.Env = []string{} // nothing of the server's environment
	err = cmd.Run()
	switch {
	case out.over:
		return Text{}, ErrTooLong
	case ctx.Err() != nil:
		return Text{}, fmt.Errorf("%w: pdftotext took too long", ErrUnreadable)
	case err != nil:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return Text{}, fmt.Errorf("%w: %s", ErrUnreadable, strings.TrimSpace(stderr.String()))
		}
		return Text{}, err
	}
	return pdfPages(out.String()), nil
}

// pdfPages numbers pdftotext's pages and tells whether they hold text at all.
func pdfPages(raw string) Text {
	pages := strings.Split(raw, "\f")
	// The last page ends in a form feed too.
	if len(pages) > 1 && strings.TrimSpace(pages[len(pages)-1]) == "" {
		pages = pages[:len(pages)-1]
	}
	var b strings.Builder
	letters := 0
	for i, page := range pages {
		fmt.Fprintf(&b, "[Page %d]\n", i+1)
		page = strings.TrimSpace(collapseBlankLines(page))
		if page != "" {
			b.WriteString(page + "\n")
		}
		b.WriteString("\n")
		for _, r := range page {
			if !unicode.IsSpace(r) {
				letters++
			}
		}
	}
	return Text{
		Content: strings.TrimSpace(b.String()) + "\n",
		Pages:   len(pages),
		Scanned: letters < scannedRunes*len(pages),
	}
}

// cappedBuffer keeps what is written up to limit and notes going over.
type cappedBuffer struct {
	bytes.Buffer
	limit int
	over  bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.Len()+len(p) > c.limit {
		c.over = true
		return 0, errors.New("too much output")
	}
	return c.Buffer.Write(p)
}
