package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
)

type Client struct {
	conn   net.Conn
	reader *bufio.Reader

	mu     sync.Mutex
	nextID uint64
}

func Dial(path string) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}

func NewClient(conn net.Conn) *Client {
	return &Client{conn: conn, reader: bufio.NewReader(conn)}
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Call(method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextID++
	id := c.nextID

	req := Request{ID: id, Method: method}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = data
	}
	if err := writeMessage(c.conn, &req); err != nil {
		return err
	}

	var resp Response
	if err := readMessage(c.reader, &resp); err != nil {
		return err
	}
	if resp.ID != id {
		return fmt.Errorf("unexpected response id %d, want %d", resp.ID, id)
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	if result != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}
