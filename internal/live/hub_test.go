package live

import (
	"errors"
	"testing"
)

func TestHubRoomCap(t *testing.T) {
	h := NewHub(2, 100)
	if _, err := h.Join("room", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Join("room", "2.2.2.2"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Join("room", "3.3.3.3"); !errors.Is(err, ErrRoomFull) {
		t.Errorf("third join: err = %v, want ErrRoomFull", err)
	}
}

func TestHubPerIPCap(t *testing.T) {
	h := NewHub(10, 2)
	ip := "9.9.9.9"
	if _, err := h.Join("r", ip); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Join("r", ip); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Join("r", ip); !errors.Is(err, ErrTooManyConns) {
		t.Errorf("third same-IP join: err = %v, want ErrTooManyConns", err)
	}
}

func TestHubBroadcastSkipsSender(t *testing.T) {
	h := NewHub(4, 100)
	a, _ := h.Join("r", "1.1.1.1")
	b, _ := h.Join("r", "2.2.2.2")

	h.Broadcast("r", a, []byte("hello"))
	select {
	case msg := <-b.Send():
		if string(msg) != "hello" {
			t.Errorf("got %q", msg)
		}
	default:
		t.Error("peer b should have received the message")
	}
	select {
	case <-a.Send():
		t.Error("sender should not receive its own broadcast")
	default:
	}
}

func TestHubLeaveFreesSlot(t *testing.T) {
	h := NewHub(1, 100)
	p, err := h.Join("r", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Join("r", "2.2.2.2"); !errors.Is(err, ErrRoomFull) {
		t.Fatalf("want room full, got %v", err)
	}
	h.Leave(p)
	if _, err := h.Join("r", "2.2.2.2"); err != nil {
		t.Errorf("join after leave: %v", err)
	}
}

func TestPeerSlowConsumerKilled(t *testing.T) {
	h := NewHub(2, 100)
	a, _ := h.Join("r", "1.1.1.1")
	b, _ := h.Join("r", "2.2.2.2")

	// Flood b past its outbound queue without draining it.
	for i := 0; i < outboundQueue+5; i++ {
		h.Broadcast("r", a, []byte("x"))
	}
	select {
	case <-b.Dead():
		// good: overwhelmed peer got dropped
	default:
		t.Error("slow consumer should have been killed")
	}
}
