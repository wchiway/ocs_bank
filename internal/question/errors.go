package question

import (
	"context"
	"errors"
)

// ErrorCategory is safe to persist or log; it never contains upstream text.
func ErrorCategory(err error) string {
	for _, entry := range []struct {
		err  error
		name string
	}{
		{context.DeadlineExceeded, "timeout"}, {context.Canceled, "canceled"},
		{ErrInvalid, "invalid"}, {ErrNotFound, "not_found"}, {ErrRefused, "refused"},
		{ErrAuth, "auth"}, {ErrRateLimit, "rate_limit"}, {ErrBusy, "busy"},
		{ErrOutputLimit, "output_limit"}, {ErrAnswerFormat, "answer_format"},
		{ErrAnswerInvalid, "answer_invalid"}, {ErrProtocol, "protocol"}, {ErrStorage, "storage"},
	} {
		if errors.Is(err, entry.err) {
			return entry.name
		}
	}
	if err == nil {
		return "ok"
	}
	return "unavailable"
}
