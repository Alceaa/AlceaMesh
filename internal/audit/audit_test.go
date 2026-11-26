package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogWritesOneJSONLinePerEntry(t *testing.T) {
	var buffer bytes.Buffer
	logger := New(&buffer)

	logger.Log(Entry{Event: "enroll", Result: "ok", DeviceID: "d1", DeviceName: "ноут", Remote: "10.0.0.9:5555"})
	logger.Log(Entry{Event: "enroll", Result: "fail", Reason: "unknown_invite", Remote: "10.0.0.9:5556"})

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %q", len(lines), buffer.String())
	}

	var first Entry
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if first.Event != "enroll" || first.Result != "ok" || first.DeviceID != "d1" {
		t.Errorf("entry = %+v", first)
	}
	if first.At.IsZero() {
		t.Error("the entry must carry a timestamp")
	}

	var second Entry
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if second.Reason != "unknown_invite" {
		t.Errorf("reason = %q", second.Reason)
	}
}

func TestFileLoggerAppendsAndProtectsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	logger, closer, err := NewFile(path)
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	logger.Log(Entry{Event: "invite", Result: "created", InviteID: "i1"})
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, closer2, err := NewFile(path)
	if err != nil {
		t.Fatalf("NewFile (second): %v", err)
	}
	second.Log(Entry{Event: "device", Result: "revoked", DeviceID: "d9"})
	if err := closer2.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2 (the file must be appended to)", len(lines))
	}
	if !strings.Contains(lines[1], "revoked") {
		t.Errorf("second line = %q", lines[1])
	}
}

func TestFileLoggerReportsBadPath(t *testing.T) {
	if _, _, err := NewFile(filepath.Join(t.TempDir(), "нет", "такой", "папки", "audit.log")); err == nil {
		t.Error("a missing folder must be reported")
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var logger *Logger
	logger.Log(Entry{Event: "enroll"})
}

func TestNowOverride(t *testing.T) {
	var buffer bytes.Buffer
	logger := New(&buffer)
	fixed := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return fixed }
	logger.Log(Entry{Event: "test"})

	var entry Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(buffer.String())), &entry); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !entry.At.Equal(fixed) {
		t.Errorf("at = %s, want %s", entry.At, fixed)
	}
}
