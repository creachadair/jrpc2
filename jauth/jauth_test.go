package jauth

import (
	"testing"
)

func TestRoundTrip(t *testing.T) {
	// Verify that a generated token can be correctly verified.
	const userName = "Zula Forthrast"
	const key = "sup4s3kr1t"
	const method = "DoAThing"
	const params = `{"x":25,"y":19,"ids":[2,3,5,7]}`

	t.Logf("Request %q params %s", method, params)

	// Generate a token.
	user := User{Name: userName, Key: []byte(key)}
	token, err := user.Token(method, []byte(params))
	if err != nil {
		t.Fatalf("Token(%q, ...) failed: %v", method, err)
	}

	t.Logf("Generated token: %#q", string(token))

	// Verify the token, this case should succeed.
	if err := user.Verify(token, method, []byte(params)); err != nil {
		t.Fatalf("Verify(%v) failed: %v", token, err)
	}

	// Verify that changing username, key, method, or parameters will cause
	// signature verification to fail.
	tests := []struct {
		tag            string
		user, key      string
		method, params string
	}{
		{"ChangeUser", "The Troll", key, method, params},
		{"ChangeKey", userName, "b0gusw3rd", method, params},
		{"ChangeMethod", userName, key, "DoAnotherThing", params},
		{"ChangeParams", userName, key, method, `{"x":22,"y":19,"ids":[2,3,5,7]}`},
		{"ChangeUserMethod", "The Troll", key, "DoAnotherThing", params},
		{"ChangeAll", "you", "shall", "not", `"pass"`},
	}
	for _, test := range tests {
		t.Run(test.tag, func(t *testing.T) {
			tu := User{Name: test.user, Key: []byte(test.key)}

			// Verify the original token with the test parameters. This should fail.
			if err := tu.Verify(token, test.method, []byte(test.params)); err != ErrInvalidSignature {
				t.Errorf("Verify(%v): got %v, want %v", token, err, ErrInvalidSignature)
			}
		})
	}

	// Check that verifying a broken token fails.
	for _, bad := range []string{
		"", `{`, `[`, `=`, // various invalid JSON
		`{"user":"x", "sig":"d2hhdGV2ZXI=", "wat":3}`, // extra fields
		`{"sig":"d2hhdGV2ZXI="}`,                      // missing user name
		`{"user":"foobar"}`,                           // missing signature
	} {
		if err := user.Verify([]byte(bad), method, []byte(params)); err == nil {
			t.Errorf("Verify(%#q) unexpectedly succeeded", bad)
		} else {
			t.Logf("Verify(%#q): successfully failed: %v", bad, err)
		}
	}
}
