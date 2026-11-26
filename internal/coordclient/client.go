package coordclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
)

type Endpoint struct {
	Name       string
	BaseURL    string
	ServerName string
}

type Signer interface {
	SignRequest(request *http.Request, body []byte, now time.Time) error
}

type Options struct {
	BaseURL   string
	Auth      string
	Endpoints []Endpoint
	TLS       *tls.Config
	Signer    Signer
	Now       func() time.Time
	Timeout   time.Duration
}

type Client struct {
	auth      string
	signer    Signer
	now       func() time.Time
	endpoints []Endpoint
	clients   []*http.Client
	cookies   map[string]string

	mu      sync.RWMutex
	current int
}

func New(baseURL, auth string) *Client {
	client, err := NewClient(Options{BaseURL: baseURL, Auth: auth})
	if err != nil {
		return &Client{auth: auth, now: time.Now, cookies: make(map[string]string)}
	}
	return client
}

func NewClient(options Options) (*Client, error) {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	endpoints := options.Endpoints
	if len(endpoints) == 0 {
		if strings.TrimSpace(options.BaseURL) == "" {
			return nil, errors.New("coordclient: no endpoints configured")
		}
		endpoints = []Endpoint{{Name: "default", BaseURL: options.BaseURL}}
	}

	clients := make([]*http.Client, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint.BaseURL) == "" {
			return nil, fmt.Errorf("coordclient: endpoint %q has no URL", endpoint.Name)
		}
		transport := &http.Transport{
			ForceAttemptHTTP2: true,
			Proxy:             http.ProxyFromEnvironment,
		}
		if options.TLS != nil {
			config := options.TLS.Clone()
			if endpoint.ServerName != "" {
				config.ServerName = endpoint.ServerName
			}
			transport.TLSClientConfig = config
		}
		clients = append(clients, &http.Client{Timeout: timeout, Transport: transport})
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		auth:      options.Auth,
		signer:    options.Signer,
		now:       now,
		endpoints: endpoints,
		clients:   clients,
		cookies:   make(map[string]string),
	}, nil
}

func (c *Client) Endpoints() []Endpoint {
	return append([]Endpoint(nil), c.endpoints...)
}

func (c *Client) ActiveEndpoint() Endpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.current < 0 || c.current >= len(c.endpoints) {
		return Endpoint{}
	}
	return c.endpoints[c.current]
}

func (c *Client) SetAuth(auth string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.auth = auth
}

func (c *Client) SetDeviceSecret(secret string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cookies["device_secret"] = secret
}

func (c *Client) deviceSecret() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cookies["device_secret"]
}

func (c *Client) authHeader() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.auth
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

type ClaimChallenge struct {
	ClaimID   string `json:"claim_id"`
	Challenge string `json:"challenge"`
	ServerKey string `json:"server_key"`
	ExpiresAt string `json:"expires_at"`
}

func (c *Client) ClaimChallenge(wgPublicKey string) (*ClaimChallenge, error) {
	var out ClaimChallenge
	if err := c.do(http.MethodPost, "/v1/claim/challenge", map[string]string{"public_key": wgPublicKey}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Claim(claimID, proof, signPublicKey, signature string) (*coordinator.Device, error) {
	body := map[string]string{
		"claim_id":        claimID,
		"proof":           proof,
		"sign_public_key": signPublicKey,
		"signature":       signature,
	}
	var out struct {
		Device coordinator.Device `json:"device"`
	}
	if err := c.do(http.MethodPost, "/v1/claim", body, &out); err != nil {
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
	var payload []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = data
	}
	return c.doWith(method, path, payload, out)
}

func (c *Client) doWith(method, path string, payload []byte, out any) error {
	if len(c.endpoints) == 0 {
		return errors.New("coordinator URL is required (--server or ALCEAMESH_COORDINATOR)")
	}

	c.mu.RLock()
	start := c.current
	c.mu.RUnlock()

	var lastErr error
	for offset := 0; offset < len(c.endpoints); offset++ {
		index := (start + offset) % len(c.endpoints)

		response, data, err := c.attempt(index, method, path, payload)
		if err != nil {
			lastErr = err
			continue
		}

		c.mu.Lock()
		c.current = index
		c.mu.Unlock()

		if response.StatusCode >= 400 {
			if rejection := identity.ParseRejection(response.StatusCode, data); rejection != nil {
				return rejection
			}
			var message struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(data, &message) == nil && message.Error != "" {
				return fmt.Errorf("%s", message.Error)
			}
			return fmt.Errorf("coordinator returned %s", response.Status)
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}

	if lastErr == nil {
		lastErr = errors.New("coordinator is unreachable")
	}
	return lastErr
}

func (c *Client) attempt(index int, method, path string, payload []byte) (*http.Response, []byte, error) {
	endpoint := c.endpoints[index]
	base := strings.TrimRight(endpoint.BaseURL, "/")

	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		return nil, nil, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if auth := c.authHeader(); auth != "" {
		request.Header.Set("Authorization", "Bearer "+auth)
	}
	if c.signer != nil {
		if err := c.signer.SignRequest(request, payload, c.now()); err != nil {
			return nil, nil, err
		}
	}

	response, err := c.clients[index].Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", endpoint.Name, err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: read response: %w", endpoint.Name, err)
	}
	return response, data, nil
}
