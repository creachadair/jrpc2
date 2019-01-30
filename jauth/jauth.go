// Package jauth defines a simple auth token generation scheme for use with the
// jrpc2 package. Tokens are signed with a shared-secret HMAC/SHA256 based on
// the content of the request. This permits the server to verify that the
// secret key associated with a given username signed the request.
//
// To generate auth tokens, attach the Token method of a User to the outbound
// request context:
//
//     user := jauth.User{Name: "foo", Key: secretKey}
//     ctx := jctx.WithAuthorizer(context.Background(), user.Token)
//
// To verify an auth token, use the Verify method of a User:
//
//     if err := user.Verify(token, method, params); err != nil {
//        log.Fatalf("Invalid token: %v", err)
//     }
//
package jauth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"
)

// A User identifies a principal, identified by a name and an encryption key.
type User struct {
	Name string
	Key  []byte

	// If set, this function is used to return the current time for the user.
	Time func() time.Time
}

func (u User) counter() int64 {
	if u.Time != nil {
		return u.Time().Unix() / 15
	}
	return time.Now().Unix() / 15
}

// Signature computes an HMAC/SHA256 signature of the given method and
// parameters.  The input to the signature is:
//
//     <user> NUL <method> NUL <params> NUL <nonce>
//
// That is the method name concatenated to the parameters, separated by a
// single NUL (0) byte.
func (u User) Signature(method string, params []byte, nonce string) []byte {
	h := hmac.New(sha256.New, u.Key)
	io.WriteString(h, u.Name)
	h.Write([]byte{0})
	io.WriteString(h, method)
	h.Write([]byte{0})
	h.Write(params)
	h.Write([]byte{0})
	io.WriteString(h, nonce)
	return h.Sum(nil)
}

// Token computes an encoded token for the given method and parameters.
func (u User) Token(ctx context.Context, method string, params []byte) ([]byte, error) {
	nonce := strconv.FormatInt(u.counter(), 10)
	return json.Marshal(Token{
		User:  u.Name,
		Nonce: nonce,
		Sig:   u.Signature(method, params, nonce),
	})
}

// ErrInvalidSignature is returned by Verify to indicate that the computed
// signature does not match the signature provided in the token.
var ErrInvalidSignature = errors.New("invalid signature")

// ErrInvalidToken is returned by Verify to indicate that the token is
// syntactically invalid.
var ErrInvalidToken = errors.New("invalid token")

// Verify decodes the specified token and checks whether its signature is valid
// for the given request. If the signature is valid, Verify returns nil. If the
// token is syntactically invalid, Verify returns ErrInvalidToken. If the
// computed signature does not match the token signature, Verify returns
// ErrInvalidSignature.
func (u User) Verify(token []byte, method string, params []byte) error {
	tok, err := ParseToken(token)
	if err != nil {
		return ErrInvalidToken
	}
	return u.VerifyParsed(tok, method, params)
}

// VerifyParsed is as verify, but applies to a parsed Token.
func (u User) VerifyParsed(tok Token, method string, params []byte) error {
	// Verify that the nonce is within one interval on either side of the
	// current counter. If not, the token has expired or the clocks of the
	// client and the server are badly skewed.
	//
	// N.B. Do the verification work regardless of whether it succeeds or fails
	// to minimize timing exposure.
	ctr := u.counter()
	nonce, err := strconv.ParseInt(tok.Nonce, 10, 64)
	want := u.Signature(method, params, tok.Nonce)
	ok := subtle.ConstantTimeCompare(tok.Sig, want) == 1

	if err != nil && tok.Nonce != "" {
		return ErrInvalidToken
	} else if nonce < ctr-1 || nonce > ctr+1 {
		return ErrInvalidToken
	} else if !ok {
		return ErrInvalidSignature
	}
	return nil
}

// A Token represents the structure of an encoded auth token, including the
// username and the signature of the request to be authorized.
type Token struct {
	User  string `json:"user"`
	Nonce string `json:"nonce"`
	Sig   []byte `json:"sig"`
}

// ParseToken decodes an encoded auth token. It reports an error if raw is not
// valid JSON, has extra fields, or is missing either required field.
func ParseToken(raw []byte) (Token, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var tok Token
	if err := dec.Decode(&tok); err != nil {
		return Token{}, err
	} else if tok.User == "" {
		return Token{}, errors.New("missing username")
	} else if len(tok.Sig) == 0 {
		return Token{}, errors.New("missing signature")
	}
	return tok, nil
}
