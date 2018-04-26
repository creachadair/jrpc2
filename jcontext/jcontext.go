// Package jcontext implements an encoder and decoder for request context
// values, allowing context metadata to be propagated through JSON-RPC.
package jcontext

import (
	"context"
	"encoding/json"
	"time"
)

// wireContext is the encoded representation of a context value. It includes
// the deadline together with an underlying payload carrying the original
// request parameters. The resulting message replaces the parameters of the
// original JSON-RPC request.
type wireContext struct {
	Deadline *time.Time      `json:"deadline,omitempty"` // encoded in UTC
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// Encode encodes the specified context and request parameters for transmission.
func Encode(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	c := wireContext{Payload: params}
	if dl, ok := ctx.Deadline(); ok {
		utcdl := dl.In(time.UTC)
		c.Deadline = &utcdl
	}
	return json.Marshal(c)
}

// Decode decodes the specified request message as a context-wrapped request,
// and returns the updated context (based on ctx) and the embedded parameters.
func Decode(ctx context.Context, req json.RawMessage) (context.Context, json.RawMessage, error) {
	var c wireContext
	if err := json.Unmarshal(req, &c); err != nil {
		return nil, nil, err
	}
	if c.Deadline != nil && !c.Deadline.IsZero() {
		var ignore context.CancelFunc
		ctx, ignore = context.WithDeadline(ctx, (*c.Deadline).In(time.UTC))
		_ = ignore // silence go vet; the caller cannot use this value
	}
	return ctx, c.Payload, nil
}
