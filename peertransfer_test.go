package main

import "testing"

func TestTransferQueueOrder(t *testing.T) {
	service := &PeerService{
		states: make(map[string]TransferProgress),
		order:  make([]string, 0, 4),
	}

	service.setTransferState("send:a#1", TransferProgress{Peer: "a", Name: "first", Queued: true})
	service.setTransferState("send:a#2", TransferProgress{Peer: "a", Name: "second"})
	service.setTransferState("send:a#3", TransferProgress{Peer: "a", Name: "broken", Error: "Передача отменена."})

	service.setTransferState("send:a#1", TransferProgress{Peer: "a", Name: "first", Sent: 7})
	if len(service.order) != 3 {
		t.Fatalf("order = %v, want 3 entries", service.order)
	}

	states := service.transferStates()
	if len(states) != 3 {
		t.Fatalf("states = %d, want 3", len(states))
	}
	if states[0].Name != "first" || states[1].Name != "second" {
		t.Errorf("pending items must keep the queue order, got %q then %q", states[0].Name, states[1].Name)
	}
	if states[0].Key != "send:a#1" {
		t.Errorf("key = %q, want %q", states[0].Key, "send:a#1")
	}
	if states[0].Sent != 7 {
		t.Errorf("sent = %d, want the updated value 7", states[0].Sent)
	}
	if states[2].Name != "broken" {
		t.Errorf("finished item must move to the end, got %q", states[2].Name)
	}

	service.ClearFinished()
	remaining := service.transferStates()
	if len(remaining) != 2 {
		t.Fatalf("after ClearFinished = %d items, want 2", len(remaining))
	}
	if remaining[0].Name != "first" || remaining[1].Name != "second" {
		t.Errorf("ClearFinished kept the wrong items: %q, %q", remaining[0].Name, remaining[1].Name)
	}
}

func TestTransferKeysAreUnique(t *testing.T) {
	service := &PeerService{
		states: make(map[string]TransferProgress),
		order:  make([]string, 0, 4),
	}

	first := service.nextTransferKey("send:peer")
	second := service.nextTransferKey("send:peer")
	if first == second {
		t.Fatalf("keys must differ, both are %q", first)
	}
	if service.nextTransferKey("receive:peer") == "" {
		t.Fatal("receive keys must be generated as well")
	}
}
