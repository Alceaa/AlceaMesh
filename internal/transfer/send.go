package transfer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/quic-go/quic-go"
)

type Progress struct {
	Name    string `json:"name"`
	Rel     string `json:"relative,omitempty"`
	Path    string `json:"path,omitempty"`
	Sent    int64  `json:"sent"`
	Total   int64  `json:"total"`
	Files   int    `json:"files,omitempty"`
	Index   int    `json:"index,omitempty"`
	Resumed bool   `json:"resumed"`
}

type ProgressFunc func(Progress)

type item struct {
	path string
	rel  string
	size int64
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
	name, items, err := collectItems(path)
	if err != nil {
		return Progress{}, err
	}
	if len(items) == 0 {
		return Progress{Name: name}, errors.New("transfer: nothing to send")
	}

	result := Progress{Name: name, Files: len(items)}
	for _, current := range items {
		result.Total += current.size
	}

	hashCtx, stopHashing := context.WithCancel(ctx)
	defer stopHashing()
	hashes := hashItems(hashCtx, items)

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

	reader := bufio.NewReader(stream)
	var done int64
	for index, current := range items {
		prepared, ok := <-hashes
		if !ok {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			return result, errors.New("transfer: hashing stopped")
		}
		if prepared.err != nil {
			return result, prepared.err
		}
		if err := sendItem(reader, stream, current, prepared.sum, done, index+1, &result, progress); err != nil {
			return result, err
		}
		done += current.size
	}

	if err := writeFrame(stream, frame{Done: true}); err != nil {
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

func sendItem(reader *bufio.Reader, stream *quic.Stream, current item, sum string, base int64, index int, result *Progress, progress ProgressFunc) error {
	file, err := os.Open(current.path)
	if err != nil {
		return err
	}
	defer file.Close()

	result.Index = index
	result.Rel = current.rel
	result.Sent = base

	head := frame{
		Name:  result.Name,
		Rel:   current.rel,
		Size:  current.size,
		SHA:   sum,
		Total: result.Total,
		Files: result.Files,
		Index: index,
	}
	if err := writeFrame(stream, head); err != nil {
		return err
	}

	answer, err := readReply(reader)
	if err != nil {
		return fmt.Errorf("transfer: read reply: %w", err)
	}
	if answer.Error != "" {
		return errors.New(answer.Error)
	}
	if answer.Offset < 0 || answer.Offset > current.size {
		return errors.New("transfer: peer requested an invalid offset")
	}
	if answer.Offset > 0 {
		if _, err := file.Seek(answer.Offset, io.SeekStart); err != nil {
			return err
		}
		result.Resumed = true
		result.Sent = base + answer.Offset
	}

	if err := streamData(file, stream, result, progress); err != nil {
		return err
	}

	confirmation, err := readReply(reader)
	if err != nil {
		return fmt.Errorf("transfer: await confirmation: %w", err)
	}
	if confirmation.Error != "" {
		return errors.New(confirmation.Error)
	}
	return nil
}

func collectItems(path string) (string, []item, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, err
	}
	name := filepath.Base(path)
	if !info.IsDir() {
		return name, []item{{path: path, rel: name, size: info.Size()}}, nil
	}

	var items []item
	err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		items = append(items, item{
			path: current,
			rel:  name + "/" + filepath.ToSlash(rel),
			size: entryInfo.Size(),
		})
		return nil
	})
	if err != nil {
		return name, nil, err
	}
	return name, items, nil
}

func streamData(file *os.File, stream *quic.Stream, result *Progress, progress ProgressFunc) error {
	buffer := takeBuffer()
	defer putBuffer(buffer)

	last := time.Now()
	for {
		n, readErr := file.Read(buffer)
		if n > 0 {
			if _, err := stream.Write(buffer[:n]); err != nil {
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

type hashResult struct {
	sum string
	err error
}

func hashItems(ctx context.Context, items []item) <-chan hashResult {
	out := make(chan hashResult, 1)
	go func() {
		defer close(out)
		for _, current := range items {
			sum, err := fileSHA256(current.path)
			select {
			case out <- hashResult{sum: sum, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return out
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
