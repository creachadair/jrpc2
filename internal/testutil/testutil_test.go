// Copyright (C) 2022 Michael J. Fromberger. All Rights Reserved.

package testutil_test

import (
	"testing"

	"github.com/creachadair/jrpc2/internal/testutil"
)

func TestParseRequest(t *testing.T) {
	t.Run("Invalid", func(t *testing.T) {
		req, err := testutil.ParseRequest(`{this is invalid}`)
		if err == nil {
			t.Errorf("ParseRequest: got %+v, wanted error", req)
		} else {
			t.Logf("Invalid OK: %v", err)
		}
	})
	t.Run("Call", func(t *testing.T) {
		req := testutil.MustParseRequest(t, `{"jsonrpc":"2.0","id":1,"method":"OK"}`)
		t.Logf("Call OK: %+v", req)
	})
	t.Run("Notification", func(t *testing.T) {
		req := testutil.MustParseRequest(t, `{"jsonrpc":"2.0","id":null,"method":"OK"}`)
		t.Logf("Note OK: %+v", req)
	})
}
