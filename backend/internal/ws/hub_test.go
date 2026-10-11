package ws

import "testing"

// 11 Oct 2026 — a browser too slow to take a broadcast is dropped without panicking the
// API: not on the next broadcast, and not when its connection then ends.
func TestASlowClientIsDroppedWithoutAPanic(t *testing.T) {
	h := NewHub()
	slow := &client{sessionID: "s1", send: make(chan []byte, 1)}
	fast := &client{sessionID: "s1", send: make(chan []byte, 8)}
	h.clients["s1"] = map[*client]bool{slow: true, fast: true}

	ev := Event{Type: EventSessionUpdated, SessionID: "s1"}
	h.Broadcast("s1", ev) // fills slow's buffer
	h.Broadcast("s1", ev) // slow is full: dropped
	h.Broadcast("s1", ev) // must not send on its closed channel

	if h.clients["s1"][slow] {
		t.Error("the slow client is still registered")
	}
	if !h.clients["s1"][fast] {
		t.Error("the fast client was dropped")
	}
	if len(fast.send) != 3 {
		t.Errorf("fast client got %d events, want 3", len(fast.send))
	}
	slow.close() // the reader ending: must not close it a second time
}
