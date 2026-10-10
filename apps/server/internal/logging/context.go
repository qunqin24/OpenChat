package logging

import (
	"context"
	"log/slog"
	"sync"
)

// Fields a request carries into every line logged with its context: the
// request id from the start, and what later middleware learns — the user, once
// the session is read. Without them a line from deep in a handler ("request
// failed", "unhandled error") cannot be tied to the request that caused it, nor
// to the person who reported it.
//
// A bag rather than a value in the context, because the context is fixed by
// the time the request logger runs and the user is known only further in:
// whoever learns something annotates the bag, and every line logged with the
// context — the logger's own completion line included — reads it.
type bag struct {
	mu    sync.Mutex
	attrs []slog.Attr
}

type bagKey struct{}

// WithFields starts a request's bag with the given attributes.
func WithFields(ctx context.Context, attrs ...slog.Attr) context.Context {
	return context.WithValue(ctx, bagKey{}, &bag{attrs: attrs})
}

// Annotate adds attributes to the request's bag, if it has one. A key already
// there is replaced.
func Annotate(ctx context.Context, attrs ...slog.Attr) {
	b, ok := ctx.Value(bagKey{}).(*bag)
	if !ok {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
outer:
	for _, attr := range attrs {
		for i, held := range b.attrs {
			if held.Key == attr.Key {
				b.attrs[i] = attr
				continue outer
			}
		}
		b.attrs = append(b.attrs, attr)
	}
}

func fieldsOf(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	b, ok := ctx.Value(bagKey{}).(*bag)
	if !ok {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]slog.Attr(nil), b.attrs...)
}

// contextHandler adds the request's fields to every record logged with its
// context, ahead of the record's own.
type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if fields := fieldsOf(ctx); len(fields) > 0 {
		r = r.Clone()
		have := map[string]bool{}
		r.Attrs(func(a slog.Attr) bool { have[a.Key] = true; return true })
		for _, f := range fields {
			if !have[f.Key] {
				r.AddAttrs(f)
			}
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
