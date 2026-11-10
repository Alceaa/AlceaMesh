package transfer

import (
	"bufio"
	"encoding/json"
	"fmt"

	"github.com/quic-go/quic-go"
)

type reply struct {
	Offset int64  `json:"offset,omitempty"`
	Error  string `json:"error,omitempty"`
}

type frame struct {
	Name  string `json:"name,omitempty"`
	Rel   string `json:"relative,omitempty"`
	Size  int64  `json:"size,omitempty"`
	SHA   string `json:"sha256,omitempty"`
	Total int64  `json:"total,omitempty"`
	Files int    `json:"files,omitempty"`
	Index int    `json:"index,omitempty"`
	Done  bool   `json:"done,omitempty"`
}

func readFrame(reader *bufio.Reader) (frame, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return frame{}, fmt.Errorf("transfer: read header: %w", err)
	}
	var value frame
	if err := json.Unmarshal(line, &value); err != nil {
		return frame{}, fmt.Errorf("transfer: decode header: %w", err)
	}
	return value, nil
}

func readReply(reader *bufio.Reader) (reply, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return reply{}, err
	}
	var value reply
	if err := json.Unmarshal(line, &value); err != nil {
		return reply{}, err
	}
	return value, nil
}

func writeFrame(stream *quic.Stream, value frame) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = stream.Write(append(data, '\n'))
	return err
}

func writeReply(stream *quic.Stream, value reply) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = stream.Write(append(data, '\n'))
	return err
}
