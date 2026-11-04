package ipc

import (
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
)

func startTestServer(t *testing.T) (*Client, func()) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := NewServer()
	server.Handle(MethodPing, func(json.RawMessage) (any, error) {
		return map[string]bool{"pong": true}, nil
	})
	server.Handle("echo", func(params json.RawMessage) (any, error) {
		var value TunnelDownParams
		if err := json.Unmarshal(params, &value); err != nil {
			return nil, err
		}
		return value, nil
	})
	server.Handle("fail", func(json.RawMessage) (any, error) {
		return nil, errors.New("boom")
	})

	go server.Serve(listener)

	client, err := DialPath(path)
	if err != nil {
		listener.Close()
		t.Fatalf("dial: %v", err)
	}
	return client, func() {
		client.Close()
		listener.Close()
	}
}

func TestCallWithoutParams(t *testing.T) {
	c, cleanup := startTestServer(t)
	defer cleanup()

	var result map[string]bool
	if err := c.Call(MethodPing, nil, &result); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !result["pong"] {
		t.Errorf("pong = false, want true")
	}
}

func TestCallWithParams(t *testing.T) {
	c, cleanup := startTestServer(t)
	defer cleanup()

	var result TunnelDownParams
	if err := c.Call("echo", TunnelDownParams{Name: "AlceaMesh"}, &result); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Name != "AlceaMesh" {
		t.Errorf("Name = %q, want AlceaMesh", result.Name)
	}
}

func TestCallHandlerError(t *testing.T) {
	c, cleanup := startTestServer(t)
	defer cleanup()

	err := c.Call("fail", nil, nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestCallUnknownMethod(t *testing.T) {
	c, cleanup := startTestServer(t)
	defer cleanup()

	if err := c.Call("does-not-exist", nil, nil); err == nil {
		t.Fatal("expected an error for an unknown method")
	}
}

func TestSequentialCalls(t *testing.T) {
	c, cleanup := startTestServer(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		if err := c.Call(MethodPing, nil, nil); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}
