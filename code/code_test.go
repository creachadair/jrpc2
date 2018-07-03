package code

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestRegistration(t *testing.T) {
	const message = "fun for the whole family"
	c := Register(-100, message)
	if got := c.String(); got != message {
		t.Errorf("Register(-100): got %q, want %q", got, message)
	} else if c != -100 {
		t.Errorf("Register(-100): got %d instead", c)
	}
}

func TestRegistrationError(t *testing.T) {
	defer func() {
		if v := recover(); v != nil {
			t.Logf("Register correctly panicked: %v", v)
		} else {
			t.Fatalf("Register should have panicked on input %d, but did not", ParseError)
		}
	}()
	Register(int32(ParseError), "bogus")
}

type testCoder Code

func (t testCoder) Code() Code  { return Code(t) }
func (testCoder) Error() string { return "bogus" }

func TestFromError(t *testing.T) {
	tests := []struct {
		input error
		want  Code
	}{
		{nil, NoError},
		{testCoder(ParseError), ParseError},
		{testCoder(InvalidRequest), InvalidRequest},
		{context.Canceled, Cancelled},
		{context.DeadlineExceeded, DeadlineExceeded},
		{errors.New("other"), SystemError},
		{io.EOF, SystemError},
	}
	for _, test := range tests {
		if got := FromError(test.input); got != test.want {
			t.Errorf("FromError(%v): got %v, want %v", test.input, got, test.want)
		}
	}
}
