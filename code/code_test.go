package code

import "testing"

func TestRegistration(t *testing.T) {
	const message = "fun for the whole family"
	c := Register(-100, message)
	if got := c.Error(); got != message {
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
