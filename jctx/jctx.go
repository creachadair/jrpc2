// Package jctx implements an encoder and decoder for request context values,
// allowing context metadata to be propagated through JSON-RPC.
package jctx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type contextKey string

const metadataKey = contextKey("jctx-metadata")

const wireVersion = "1"

// wireContext is the encoded representation of a context value. It includes
// the deadline together with an underlying payload carrying the original
// request parameters. The resulting message replaces the parameters of the
// original JSON-RPC request.
type wireContext struct {
	V string `json:"jctx"` // must be wireVersion

	Deadline *time.Time      `json:"deadline,omitempty"` // encoded in UTC
	Payload  json.RawMessage `json:"payload,omitempty"`
	Metadata json.RawMessage `json:"meta,omitempty"`
}

// Encode encodes the specified context and request parameters for transmission.
// If a deadline is set on ctx, it is converted to UTC before encoding.
// If metadata are set on ctx (see jctx.WithMetadata), they are included.
func Encode(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	c := wireContext{V: wireVersion, Payload: params}
	if dl, ok := ctx.Deadline(); ok {
		utcdl := dl.In(time.UTC)
		c.Deadline = &utcdl
	}
	if v := ctx.Value(metadataKey); v != nil {
		c.Metadata = v.(json.RawMessage)
	}
	return json.Marshal(c)
}

// Decode decodes the specified request message as a context-wrapped request,
// and returns the updated context (based on ctx) and the embedded parameters.
//
// If the encoded request specifies a deadline, that deadline is set in the
// context value returned.
//
// If the request includes context metadata, they are attached and can be
// recovered using jctx.UnmarshalMetadata.
func Decode(ctx context.Context, req json.RawMessage) (context.Context, json.RawMessage, error) {
	var c wireContext
	if err := json.Unmarshal(req, &c); err != nil {
		return nil, nil, err
	} else if c.V != wireVersion {
		return nil, nil, fmt.Errorf("invalid context wire version %q", c.V)
	}
	if c.Metadata != nil {
		ctx = context.WithValue(ctx, metadataKey, c.Metadata)
	}
	if c.Deadline != nil && !c.Deadline.IsZero() {
		var ignored context.CancelFunc
		ctx, ignored = context.WithDeadline(ctx, (*c.Deadline).In(time.UTC))
		_ = ignored // the caller cannot use this value
	}

	return ctx, c.Payload, nil
}

// WithMetadata attaches the specified metadata value to the context.  The meta
// value must support encoding to JSON. In case of error, the original value of
// ctx is returned along with the error.
func WithMetadata(ctx context.Context, meta interface{}) (context.Context, error) {
	bits, err := json.Marshal(meta)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, metadataKey, json.RawMessage(bits)), nil
}

// UnmarshalMetadata decodes the metadata value attached to ctx into meta, or
// returns ErrNoMetadata if ctx does not have metadata attached.
func UnmarshalMetadata(ctx context.Context, meta interface{}) error {
	if v := ctx.Value(metadataKey); v != nil {
		return json.Unmarshal(v.(json.RawMessage), meta)
	}
	return ErrNoMetadata
}

// ErrNoMetadata is returned by the Metadata function if the context does not
// contain a metadata value.
var ErrNoMetadata = errors.New("context metadata not present")
