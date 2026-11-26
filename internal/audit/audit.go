package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

type Entry struct {
	At         time.Time `json:"at"`
	Event      string    `json:"event"`
	DeviceID   string    `json:"deviceID,omitempty"`
	DeviceName string    `json:"deviceName,omitempty"`
	InviteID   string    `json:"inviteID,omitempty"`
	Remote     string    `json:"remote,omitempty"`
	Result     string    `json:"result"`
	Reason     string    `json:"reason,omitempty"`
	Detail     string    `json:"detail,omitempty"`
}

type Logger struct {
	mu  sync.Mutex
	out io.Writer
	now func() time.Time
}

func New(out io.Writer) *Logger {
	return &Logger{out: out, now: time.Now}
}

func NewFile(path string) (*Logger, io.Closer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	return New(file), file, nil
}

func NewStdLogger() *Logger {
	return New(&standardWriter{})
}

func (l *Logger) Log(entry Entry) {
	if l == nil || l.out == nil {
		return
	}
	if entry.At.IsZero() {
		entry.At = l.now()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(append(data, '\n'))
}

type standardWriter struct{}

func (w *standardWriter) Write(data []byte) (int, error) {
	log.Print("audit " + string(data[:len(data)-1]))
	return len(data), nil
}
