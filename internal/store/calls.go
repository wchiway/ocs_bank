package store

import (
	"context"
	"github.com/wchiway/ocs_bank/internal/telemetry"
)

func (s *SQLite) RecordCall(ctx context.Context, c telemetry.Call) error {
	var input, output any
	if c.UsageKnown {
		input, output = c.InputTokens, c.OutputTokens
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO upstream_calls(protocol,model,started_at,duration_ms,attempt,http_status,result,input_tokens,output_tokens) VALUES(?,?,?,?,?,?,?,?,?)`, c.Protocol, c.Model, c.Started.Unix(), c.Duration.Milliseconds(), c.Attempt, c.HTTPStatus, c.Result, input, output)
	return err
}
