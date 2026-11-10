package transfer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/quic-go/quic-go"
)

type Progress struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Sent    int64  `json:"sent"`
	Total   int64  `json:"total"`
	Resumed bool   `json:"resumed"`
}

type ProgressFunc func(Progress)

type header struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	SHA  string `json:"sha256"`
}

type reply struct {
	Offset int64  `json:"offset"`
	Error  string `json:"error,omitempty"`
}

func Send(ctx context.Context, pc net.PacketConn, cert tls.Certificate, addr net.Addr, path string, progress ProgressFunc) (Progress, error) {
	session, err := NewSession(pc, cert)
	if err != nil {
		return Progress{}, err
	}
	defer session.Close()

	return session.Send(ctx, addr, path, progress)
}

func (s *Session) Send(ctx context.Context, addr net.Addr, path string, progress ProgressFunc) (Progress, error) {
	file, err := os.Open(path)
	if err != nil {
		return Progress{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return Progress{}, err
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return Progress{}, err
	}

	result := Progress{Name: filepath.Base(path), Total: info.Size()}

	conn, err := s.transport.Dial(ctx, addr, tlsConfig(s.cert), quicConfig())
	if err != nil {
		return result, fmt.Errorf("transfer: connect to peer: %w", err)
	}
	defer conn.CloseWithError(0, "")

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return result, err
	}
	defer stream.Close()

	head, err := json.Marshal(header{Name: result.Name, Size: info.Size(), SHA: sum})
	if err != nil {
		return result, err
	}
	if _, err := stream.Write(append(head, '\n')); err != nil {
		return result, err
	}

	reader := bufio.NewReader(stream)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return result, fmt.Errorf("transfer: read reply: %w", err)
	}
	var answer reply
	if err := json.Unmarshal(line, &answer); err != nil {
		return result, fmt.Errorf("transfer: decode reply: %w", err)
	}
	if answer.Error != "" {
		return result, errors.New(answer.Error)
	}
	if answer.Offset < 0 || answer.Offset > info.Size() {
		return result, errors.New("transfer: peer requested an invalid offset")
	}
	if answer.Offset > 0 {
		if _, err := file.Seek(answer.Offset, io.SeekStart); err != nil {
			return result, err
		}
		result.Resumed = true
		result.Sent = answer.Offset
	}

	if err := streamData(file, stream, &result, progress); err != nil {
		return result, err
	}

	confirmation, err := reader.ReadBytes('\n')
	if err != nil {
		return result, fmt.Errorf("transfer: await confirmation: %w", err)
	}
	var ack reply
	if err := json.Unmarshal(confirmation, &ack); err != nil {
		return result, fmt.Errorf("transfer: decode confirmation: %w", err)
	}
	if ack.Error != "" {
		return result, errors.New(ack.Error)
	}

	if err := writeReply(stream, reply{}); err != nil {
		return result, err
	}
	if err := stream.Close(); err != nil {
		return result, err
	}

	select {
	case <-conn.Context().Done():
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
	}
	if progress != nil {
		progress(result)
	}
	return result, nil
}

func streamData(file *os.File, stream *quic.Stream, result *Progress, progress ProgressFunc) error {
	buf := make([]byte, 256<<10)
	last := time.Now()
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			if _, err := stream.Write(buf[:n]); err != nil {
				return fmt.Errorf("transfer: send data: %w", err)
			}
			result.Sent += int64(n)
			if progress != nil && time.Since(last) > 100*time.Millisecond {
				progress(*result)
				last = time.Now()
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
