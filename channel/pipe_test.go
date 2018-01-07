package channel

import (
	"sync"
	"testing"
)

func TestPipe(t *testing.T) {
	lhs, rhs := Pipe()
	defer lhs.Close()
	defer rhs.Close()

	const message1 = `["Full plate and packing steel"]`

	var wg sync.WaitGroup
	var lhsSendErr, lhsRecvErr, rhsSendErr, rhsRecvErr error
	var lhsgot, rhsgot []byte

	wg.Add(1)
	go func() {
		defer wg.Done()
		lhsSendErr = lhs.Send([]byte(message1))
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		rhsgot, rhsRecvErr = rhs.Recv()
	}()
	wg.Wait()

	if lhsSendErr != nil {
		t.Errorf("Send (left): %v", lhsSendErr)
	}
	if rhsRecvErr != nil {
		t.Errorf("Recv (right): %v", rhsRecvErr)
	}
	if got, want := string(rhsgot), message1; got != want {
		t.Errorf("Message (right): got %#q, want %#q", got, want)
	}

	const message2 = `{"slogan":"Jump on your sword, evil!"}`

	wg.Add(1)
	go func() {
		defer wg.Done()
		rhsSendErr = rhs.Send([]byte(message2))
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		lhsgot, lhsRecvErr = lhs.Recv()
	}()
	wg.Wait()

	if rhsSendErr != nil {
		t.Errorf("Send (right): %v", rhsSendErr)
	}
	if lhsRecvErr != nil {
		t.Errorf("Recv (left): %v", lhsRecvErr)
	}
	if got, want := string(lhsgot), message2; got != want {
		t.Errorf("Message (left): got %#q, want %#q", got, want)
	}
}
