package client

import (
	"alceacore.ru/alceamesh/internal/ipc"
)

type Client struct {
	conn *ipc.Client
}

func Dial() (*Client, error) {
	conn, err := ipc.DialPath(ipc.SocketPath())
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Ping() error {
	return c.conn.Call(ipc.MethodPing, nil, nil)
}

func (c *Client) Version() (*ipc.VersionResult, error) {
	var result ipc.VersionResult
	if err := c.conn.Call(ipc.MethodVersion, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Status() (*ipc.StatusResult, error) {
	var result ipc.StatusResult
	if err := c.conn.Call(ipc.MethodStatus, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) TunnelUp(params ipc.TunnelUpParams) (*ipc.TunnelInfo, error) {
	var info ipc.TunnelInfo
	if err := c.conn.Call(ipc.MethodTunnelUp, params, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) TunnelDown(name string) error {
	return c.conn.Call(ipc.MethodTunnelDown, ipc.TunnelDownParams{Name: name}, nil)
}
