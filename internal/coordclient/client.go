package coordclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"alceacore.ru/alceamesh/internal/coordinator"
)

type Client struct {
	baseURL string
	auth    string
	http    *http.Client
}

func New(baseURL, auth string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		auth:    auth,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

type EnrollResult struct {
	Device coordinator.Device `json:"device"`
	Secret string             `json:"secret"`
}

func (c *Client) Ping() error {
	return c.do(http.MethodGet, "/v1/ping", nil, nil)
}

func (c *Client) Version() (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := c.do(http.MethodGet, "/v1/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

func (c *Client) Enroll(token, name, publicKey, platform string) (*EnrollResult, error) {
	body := map[string]string{
		"token":      token,
		"name":       name,
		"public_key": publicKey,
		"platform":   platform,
	}
	var out EnrollResult
	if err := c.do(http.MethodPost, "/v1/enroll", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Heartbeat() error {
	return c.do(http.MethodPost, "/v1/heartbeat", nil, nil)
}

func (c *Client) Devices() ([]coordinator.DeviceView, error) {
	var out struct {
		Devices []coordinator.DeviceView `json:"devices"`
	}
	if err := c.do(http.MethodGet, "/v1/devices", nil, &out); err != nil {
		return nil, err
	}
	return out.Devices, nil
}

func (c *Client) CreateToken(label, meshIP string, ttl time.Duration) (*coordinator.Token, error) {
	body := map[string]any{
		"label":       label,
		"mesh_ip":     meshIP,
		"ttl_seconds": int(ttl.Seconds()),
	}
	var out struct {
		Token coordinator.Token `json:"token"`
	}
	if err := c.do(http.MethodPost, "/v1/tokens", body, &out); err != nil {
		return nil, err
	}
	return &out.Token, nil
}

func (c *Client) Tokens() ([]coordinator.Token, error) {
	var out struct {
		Tokens []coordinator.Token `json:"tokens"`
	}
	if err := c.do(http.MethodGet, "/v1/tokens", nil, &out); err != nil {
		return nil, err
	}
	return out.Tokens, nil
}

func (c *Client) DeleteDevice(id string) error {
	return c.do(http.MethodDelete, "/v1/devices/"+id, nil, nil)
}

func (c *Client) do(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.auth != "" {
		req.Header.Set("Authorization", "Bearer "+c.auth)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &payload) == nil && payload.Error != "" {
			return fmt.Errorf("%s", payload.Error)
		}
		return fmt.Errorf("coordinator returned %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
