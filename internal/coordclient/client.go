package coordclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
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

type PresenceRequest struct {
	PublicKey string `json:"public_key"`
	Name      string `json:"name"`
	MeshIP    string `json:"mesh_ip"`
	Platform  string `json:"platform"`
	Available bool   `json:"available"`
}

func (c *Client) Announce(req PresenceRequest) (*coordinator.Device, error) {
	var out struct {
		Device coordinator.Device `json:"device"`
	}
	if err := c.do(http.MethodPost, "/v1/presence", req, &out); err != nil {
		return nil, err
	}
	return &out.Device, nil
}

func (c *Client) Peers(availableOnly bool) ([]coordinator.DeviceView, error) {
	path := "/v1/peers"
	if availableOnly {
		path += "?available=1"
	}
	var out struct {
		Peers []coordinator.DeviceView `json:"peers"`
	}
	if err := c.do(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Peers, nil
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
	return c.devices(false)
}

func (c *Client) DevicesPending() ([]coordinator.DeviceView, error) {
	return c.devices(true)
}

func (c *Client) devices(pending bool) ([]coordinator.DeviceView, error) {
	path := "/v1/devices"
	if pending {
		path += "?pending=1"
	}
	var out struct {
		Devices []coordinator.DeviceView `json:"devices"`
	}
	if err := c.do(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Devices, nil
}

func (c *Client) EnrollWithInvite(code, name, platform, publicKey, signPublicKey string, signer *identity.Key) (*EnrollResult, error) {
	payload := identity.CanonicalEnroll(code, name, platform, publicKey, signPublicKey)
	body := map[string]string{
		"invite":          code,
		"name":            name,
		"platform":        platform,
		"public_key":      publicKey,
		"sign_public_key": signPublicKey,
		"signature":       identity.SignMessage(signer, payload),
	}
	var out EnrollResult
	if err := c.do(http.MethodPost, "/v1/enroll", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateInvite(label, meshIP string, uses int, ttl time.Duration) (*coordinator.Invitation, string, error) {
	body := map[string]any{
		"label":       label,
		"mesh_ip":     meshIP,
		"uses":        uses,
		"ttl_seconds": int(ttl.Seconds()),
	}
	var out struct {
		Invitation coordinator.Invitation `json:"invitation"`
		Code       string                 `json:"code"`
	}
	if err := c.do(http.MethodPost, "/v1/invites", body, &out); err != nil {
		return nil, "", err
	}
	return &out.Invitation, out.Code, nil
}

func (c *Client) Invites() ([]coordinator.Invitation, error) {
	var out struct {
		Invitations []coordinator.Invitation `json:"invitations"`
	}
	if err := c.do(http.MethodGet, "/v1/invites", nil, &out); err != nil {
		return nil, err
	}
	return out.Invitations, nil
}

func (c *Client) RevokeInvite(id string) (*coordinator.Invitation, error) {
	var out struct {
		Invitation coordinator.Invitation `json:"invitation"`
	}
	if err := c.do(http.MethodPost, "/v1/invites/"+url.PathEscape(id)+"/revoke", nil, &out); err != nil {
		return nil, err
	}
	return &out.Invitation, nil
}

func (c *Client) RevokeDevice(id string) (*coordinator.Device, error) {
	var out struct {
		Device coordinator.Device `json:"device"`
	}
	if err := c.do(http.MethodPost, "/v1/devices/"+url.PathEscape(id)+"/revoke", nil, &out); err != nil {
		return nil, err
	}
	return &out.Device, nil
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

func (c *Client) PostSignal(_ context.Context, signal coordinator.Signal) error {
	return c.do(http.MethodPost, "/v1/signal", signal, nil)
}

func (c *Client) Signals(_ context.Context, peer string) ([]coordinator.Signal, error) {
	var out struct {
		Signals []coordinator.Signal `json:"signals"`
	}
	if err := c.do(http.MethodGet, "/v1/signal?peer="+url.QueryEscape(peer), nil, &out); err != nil {
		return nil, err
	}
	return out.Signals, nil
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
