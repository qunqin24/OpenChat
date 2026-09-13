package llm

import (
	"errors"
	"fmt"
)

// ErrorKind is the category a failure falls into. The run layer retries, gives
// up or asks the user to fix something based on this, so the categories are cut
// by what can be done about them rather than by which HTTP status arrived.
type ErrorKind string

const (
	// ErrorTransport never reached the model. Nothing was generated and nothing
	// was billed, which is the only situation where a blind retry is honest.
	ErrorTransport ErrorKind = "transport"

	// ErrorAuth is a credential the operator has to fix. Retrying it just locks
	// the key out faster, and falling back to another provider would move the
	// user's data somewhere they did not agree to.
	ErrorAuth ErrorKind = "auth"

	// ErrorRateLimit is the provider asking us to slow down.
	ErrorRateLimit ErrorKind = "rate_limit"

	// ErrorProvider is the endpoint refusing or failing on its own terms.
	ErrorProvider ErrorKind = "provider"

	// ErrorProtocol is a stream that is not what it claims to be: unparsable
	// frames, a response that ends without saying why. It is never retryable
	// mid-stream — content has already been published.
	ErrorProtocol ErrorKind = "protocol"

	// ErrorContextLength is the request being too large for the model. The fix
	// is a different context, not a second attempt at the same one.
	ErrorContextLength ErrorKind = "context_length"
)

// Error is a provider failure with enough structure for the run layer to act
// on. Message is written for operators and logs; the HTTP layer maps it to
// something localised before a browser sees it, and raw provider bodies,
// headers and credentials never travel in it.
type Error struct {
	Kind ErrorKind

	// Code is the provider's own error code when it gave one, kept verbatim
	// because it is what their support will ask for.
	Code string

	Message string

	// Retryable is only meaningful before the first delta has been published.
	// Once the user has seen output, replaying the turn duplicates it.
	Retryable bool

	cause error
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("llm: %s", e.Kind)
	if e.Code != "" {
		msg += " (" + e.Code + ")"
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.cause != nil {
		msg += ": " + e.cause.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.cause }

// Errorf builds an Error whose cause stays inspectable by errors.Is.
func Errorf(kind ErrorKind, cause error, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...), cause: cause}
}

// IsKind reports whether err is an *Error of the given kind.
func IsKind(err error, kind ErrorKind) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == kind
}
