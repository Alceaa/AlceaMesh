package transfer

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
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
	s.setRemote(conn.RemoteAddr())

	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return Progress{}, err
	}

	reader := bufio.NewReader(stream)
	limit := s.receivePacer()
	result := Progress{}
	for {
		head, err := readFrame(reader)
		if err != nil {
			return result, err
		}
		if head.Done {
			return result, nil
		}
		if err := receiveFile(ctx, reader, stream, limit, inboxDir, head, &result, progress); err != nil {
			return result, err
		}
	}
}

func receiveFile(ctx context.Context, reader *bufio.Reader, stream *quic.Stream, limit *pacer, inboxDir string, head frame, result *Progress, progress ProgressFunc) error {
	rel, err := checkFrame(head)
	if err != nil {
		_ = writeReply(stream, reply{Error: err.Error()})
		return err
	}

	finalPath := filepath.Join(inboxDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		_ = writeReply(stream, reply{Error: err.Error()})
		return err
	}
	partPath := finalPath + partSuffix

	var offset int64
	if info, err := os.Stat(partPath); err == nil {
		if info.Size() <= head.Size {
			offset = info.Size()
		} else {
			_ = os.Remove(partPath)
		}
	}
	if err := writeReply(stream, reply{Offset: offset}); err != nil {
		return err
	}

	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		file.Close()
		return err
	}

	result.Name = head.Name
	result.Rel = head.Rel
	result.Total = head.Total
	result.Files = head.Files
	result.Index = head.Index
	result.Sent += offset
	if offset > 0 {
		result.Resumed = true
	}
	if progress != nil {
		progress(*result)
	}

	if err := receiveData(ctx, limit, reader, file, head.Size-offset, result, progress); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	if err := verifyChecksum(partPath, head.SHA); err != nil {
		_ = os.Remove(partPath)
		_ = writeReply(stream, reply{Error: err.Error()})
		return err
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = writeReply(stream, reply{Error: err.Error()})
		return err
	}
	result.Path = finalPath

	return writeReply(stream, reply{})
}

func checkFrame(head frame) (string, error) {
	rel := strings.TrimSpace(head.Rel)
	if rel == "" {
		rel = strings.TrimSpace(head.Name)
	}
	if err := checkRel(rel); err != nil {
		return "", err
	}
	if head.Size < 0 {
		return "", errors.New("transfer: negative file size")
	}
	if strings.TrimSpace(head.SHA) == "" {
		return "", errors.New("transfer: sender did not provide a checksum")
	}
	return rel, nil
}

func checkRel(rel string) error {
	if rel == "" || rel == "." || rel == ".." {
		return errors.New("transfer: invalid file name")
	}
	if strings.ContainsAny(rel, `:\`) {
		return errors.New("transfer: file name must not contain a path")
	}
	for _, part := range strings.Split(rel, "/") {
		if err := checkName(part); err != nil {
			return err
		}
	}
	return nil
}

func receiveData(ctx context.Context, limit *pacer, reader io.Reader, file *os.File, remaining int64, result *Progress, progress ProgressFunc) error {
	buffer := takeBuffer()
	defer putBuffer(buffer)

	last := time.Now()

	for remaining > 0 {
		chunk := int64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		n, readErr := reader.Read(buffer[:chunk])
		if n > 0 {
			if _, err := file.Write(buffer[:n]); err != nil {
				return err
			}
			if err := limit.wait(ctx, n); err != nil {
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
