package channel

import (
	"sync"
	"testing"
)

type sender interface {
	Send([]byte) error
}
type receiver interface {
	Recv() ([]byte, error)
}

func testSendRecv(t *testing.T, s sender, r receiver, msg string) {
	var wg sync.WaitGroup
	var sendErr, recvErr error
	var data []byte

	wg.Add(2)
	go func() {
		defer wg.Done()
		data, recvErr = r.Recv()
	}()
	go func() {
		defer wg.Done()
		sendErr = s.Send([]byte(msg))
	}()
	wg.Wait()

	if sendErr != nil {
		t.Errorf("Send(%q): unexpected error: %v", msg, sendErr)
	}
	if recvErr != nil {
		t.Errorf("Recv(): unexpected error: %v", recvErr)
	}
	if got := string(data); got != msg {
		t.Errorf("Recv(): got %q, want %q", got, msg)
	}
}

func TestPipe(t *testing.T) {
	lhs, rhs := Pipe()
	defer lhs.Close()
	defer rhs.Close()

	const message1 = `["Full plate and packing steel"]`
	const message2 = `{"slogan":"Jump on your sword, evil!"}`

	t.Logf("Testing lhs ⇒ rhs :: %q", message1)
	testSendRecv(t, lhs, rhs, message1)
	t.Logf("Testing rhs ⇒ lhs :: %q", message2)
	testSendRecv(t, rhs, lhs, message2)
}
