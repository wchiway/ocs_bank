package telemetry

import (
	"context"
	"time"
)

type Call struct {
	Protocol     string
	Model        string
	Started      time.Time
	Duration     time.Duration
	Attempt      int
	HTTPStatus   int
	Result       string
	UsageKnown   bool
	InputTokens  int64
	OutputTokens int64
}

type Recorder interface {
	RecordCall(context.Context, Call) error
}
