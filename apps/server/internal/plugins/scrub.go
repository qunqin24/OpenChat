package plugins

import (
	"errors"
	"strings"
)

// A service may echo the credential it was sent back in an error - a JSON-RPC
// message quoting the request, a proxy's page - and errors from here go into
// the logs and, for a failed call, to the model. So whatever leaves this
// package is first cleared of it: the header's whole value and, for a bearer
// value, the token alone.

const redacted = "[redacted]"

// secretsOf is what must not be seen of an endpoint's credential.
func secretsOf(end Endpoint) []string {
	value := strings.TrimSpace(end.HeaderValue)
	if value == "" {
		return nil
	}
	out := []string{value}
	if scheme, token, ok := strings.Cut(value, " "); ok && strings.EqualFold(scheme, "bearer") && strings.TrimSpace(token) != "" {
		out = append(out, strings.TrimSpace(token))
	}
	return out
}

// scrubText is text with the endpoint's credential taken out.
func scrubText(text string, end Endpoint) string {
	for _, secret := range secretsOf(end) {
		text = strings.ReplaceAll(text, secret, redacted)
	}
	return text
}

// scrubbedError is an error whose text had the credential taken out; it is
// still ErrRefused or ErrForbidden when the original was.
type scrubbedError struct {
	msg  string
	kind error
}

func (e scrubbedError) Error() string { return e.msg }
func (e scrubbedError) Unwrap() error { return e.kind }

// scrub is err with the endpoint's credential taken out of its text.
func scrub(err error, end Endpoint) error {
	if err == nil {
		return nil
	}
	msg := scrubText(err.Error(), end)
	if msg == err.Error() {
		return err
	}
	var kind error
	switch {
	case errors.Is(err, ErrRefused):
		kind = ErrRefused
	case errors.Is(err, ErrForbidden):
		kind = ErrForbidden
	}
	return scrubbedError{msg: msg, kind: kind}
}
