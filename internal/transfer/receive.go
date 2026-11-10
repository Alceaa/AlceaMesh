package transfer

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const partSuffix = ".part"

func Receive(ctx context.Context, pc net.PacketConn, cert tls.Certificate, inboxDir string, progress ProgressFunc) (Progress, error) {
	session, err := NewSession(pc, cert)
	if err != nil {
		return Progress{}, err
	}
	defer session.Close()

	return session.Receive(ctx, inboxDir, progress)
}

func (s *Session) Receive(ctx context.Context, inboxDir string, progress ProgressFunc) (Progress, error) {
	conn, err := s.listener.Accept(ctx)
	if err != nil {
		return Progress{}, err
	}
	defer conn.CloseWithError(0, "")

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return Progress{}, err
	}
	defer stream.Close()

	reader := bufio.NewReader(stream)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return Progress{}, fmt.Errorf("transfer: read header: %w", err)
	}

	var head header
	if err := json.Unmarshal(line, &head); err != nil {
		return Progress{}, fmt.Errorf("transfer: decode header: %w", err)
	}
	if err := checkName(head.Name); err != nil {
		_ = writeReply(stream, reply{Error: err.Error()})
		return Progress{}, err
	}
	if head.Size < 0 {
		err := errors.New("transfer: negative file size")
		_ = writeReply(stream, reply{Error: err.Error()})
		return Progress{}, err
	}
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		_ = writeReply(stream, reply{Error: err.Error()})
		return Progress{}, err
	}

	finalPath := filepath.Join(inboxDir, head.Name)
	partPath := finalPath + partSuffix

	var offset int64
	if info, err := os.Stat(partPath); err == nil && info.Size() <= head.Size {
		offset = info.Size()
	}
	if err := writeReply(stream, reply{Offset: offset}); err != nil {
		return Progress{}, err
	}

	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Progress{}, err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		file.Close()
		return Progress{}, err
	}

	result := Progress{Name: head.Name, Total: head.Size, Sent: offset, Resumed: offset > 0}
	if err := receiveData(reader, file, head.Size-offset, &result, progress); err != nil {
		file.Close()
		return result, err
	}
	if err := file.Close(); err != nil {
		return result, err
	}
	if progress != nil {
		progress(result)
	}

	if err := verifyChecksum(partPath, head.SHA); err != nil {
		_ = writeReply(stream, reply{Error: err.Error()})
		return result, err
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = writeReply(stream, reply{Error: err.Error()})
		return result, err
	}
	result.Path = finalPath

	if err := writeReply(stream, reply{}); err != nil {
		return result, err
	}
	if _, err := reader.ReadBytes('\n'); err != nil {
		return result, fmt.Errorf("transfer: await acknowledgement: %w", err)
	}
	return result, nil
}

func receiveData(reader io.Reader, file *os.File, remaining int64, result *Progress, progress ProgressFunc) error {
	buf := make([]byte, 256<<10)
	last := time.Now()

	for remaining > 0 {
		chunk := int64(len(buf))
		if remaining < chunk {
			chunk = remaining
		}
		n, readErr := reader.Read(buf[:chunk])
		if n > 0 {
			if _, err := file.Write(buf[:n]); err != nil {
				return err
			}
			remaining -= int64(n)
			result.Sent += int64(n)
			if progress != nil && time.Since(last) > 100*time.Millisecond {
				progress(*result)
				last = time.Now()
			}
		}
		if readErr != nil {
			return fmt.Errorf("transfer: connection lost: %w", readErr)
		}
	}
	return nil
}

func writeReply(w io.Writer, value reply) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

func checkName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return errors.New("transfer: invalid file name")
	}
	if strings.ContainsAny(name, "/\\") || filepath.Base(name) != name {
		return errors.New("transfer: file name must not contain a path")
	}
	return nil
}

func verifyChecksum(path, want string) error {
	if strings.TrimSpace(want) == "" {
		return errors.New("transfer: sender did not provide a checksum")
	}
	got, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("transfer: checksum mismatch (got %s, want %s)", got, want)
	}
	return nil
}
