package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"alceacore.ru/alceamesh/internal/audit"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
)

const maxBodyBytes = 1 << 20

type Server struct {
	Registry *coordinator.Registry
	AdminKey string
	Version  string

	RequireSignature bool
	Nonces           *identity.NonceCache
	Now              func() time.Time
	Limiter          *Limiter
	Audit            *audit.Logger

	claimStore *claimStore
}

func (s *Server) auditRequest(r *http.Request, entry audit.Entry) {
	if s == nil || s.Audit == nil {
		return
	}
	entry.Remote = remoteOf(r)
	s.Audit.Log(entry)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ping", s.handlePing)
	mux.HandleFunc("GET /v1/version", s.handleVersion)
	mux.HandleFunc("POST /v1/enroll", s.withRateLimit(s.handleEnroll))
	mux.HandleFunc("POST /v1/claim/challenge", s.withRateLimit(s.handleClaimChallenge))
	mux.HandleFunc("POST /v1/claim", s.withRateLimit(s.handleClaim))
	mux.HandleFunc("POST /v1/invites", s.handleCreateInvite)
	mux.HandleFunc("GET /v1/invites", s.handleListInvites)
	mux.HandleFunc("POST /v1/invites/{id}/revoke", s.handleRevokeInvite)
	mux.HandleFunc("POST /v1/devices/{id}/revoke", s.handleRevokeDevice)
	mux.HandleFunc("POST /v1/heartbeat", s.withSignature(s.handleHeartbeat))
	mux.HandleFunc("GET /v1/devices", s.withSignature(s.handleListDevices))
	mux.HandleFunc("DELETE /v1/devices/{id}", s.withSignature(s.handleDeleteDevice))
	mux.HandleFunc("POST /v1/tokens", s.withSignature(s.handleCreateToken))
	mux.HandleFunc("GET /v1/tokens", s.withSignature(s.handleListTokens))
	mux.HandleFunc("POST /v1/presence", s.withSignature(s.handlePresence))
	mux.HandleFunc("GET /v1/peers", s.withSignature(s.handlePeers))
	mux.HandleFunc("POST /v1/signal", s.withSignature(s.handlePostSignal))
	mux.HandleFunc("GET /v1/signal", s.withSignature(s.handleTakeSignals))
	return mux
}

func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ping", s.handlePing)
	mux.HandleFunc("GET /v1/version", s.handleVersion)
	mux.HandleFunc("POST /v1/enroll", s.withRateLimit(s.handleEnroll))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.auditRequest(r, audit.Entry{Event: "public", Result: "deny", Reason: "not_public", Detail: r.URL.Path})
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error":  "этот адрес доступен только из сети Alcea",
			"reason": "not_public",
		})
	})
	return mux
}

func (s *Server) handlePing(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"pong": true})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.Version})
}

type enrollRequest struct {
	Token         string `json:"token"`
	Invite        string `json:"invite"`
	Name          string `json:"name"`
	PublicKey     string `json:"public_key"`
	SignPublicKey string `json:"sign_public_key"`
	Signature     string `json:"signature"`
	Platform      string `json:"platform"`
}

type enrollResponse struct {
	Device coordinator.Device `json:"device"`
	Secret string             `json:"secret"`
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Invite != "" {
		s.handleInviteEnroll(w, r, inviteEnrollRequest{
			Invite:        req.Invite,
			Name:          req.Name,
			Platform:      req.Platform,
			PublicKey:     req.PublicKey,
			SignPublicKey: req.SignPublicKey,
			Signature:     req.Signature,
		})
		return
	}

	device, err := s.Registry.Enroll(coordinator.EnrollRequest{
		Token:         req.Token,
		Name:          req.Name,
		PublicKey:     req.PublicKey,
		SignPublicKey: req.SignPublicKey,
		Platform:      req.Platform,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	writeJSON(w, http.StatusCreated, enrollResponse{Device: *device, Secret: device.Secret})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	secret, ok := s.bearer(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("missing bearer token"))
		return
	}
	device, err := s.Registry.Heartbeat(secret)
	if err != nil {
		if writeRegistryAuthError(w, err) {
			return
		}
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": device})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}
	devices, err := s.Registry.Devices()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if r.URL.Query().Get("pending") == "1" {
		pending := make([]coordinator.DeviceView, 0, len(devices))
		for _, device := range devices {
			if device.Attachable() {
				pending = append(pending, device)
			}
		}
		devices = pending
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if err := s.Registry.DeleteDevice(r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type createTokenRequest struct {
	Label      string `json:"label"`
	MeshIP     string `json:"mesh_ip"`
	TTLSeconds int    `json:"ttl_seconds"`
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req createTokenRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var ttl time.Duration
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	token, err := s.Registry.CreateToken(req.Label, req.MeshIP, ttl)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token})
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	tokens, err := s.Registry.Tokens()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

type presenceRequest struct {
	PublicKey string `json:"public_key"`
	Name      string `json:"name"`
	MeshIP    string `json:"mesh_ip"`
	Platform  string `json:"platform"`
	Available bool   `json:"available"`
}

func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	var req presenceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	device, err := s.Registry.Announce(coordinator.AnnounceRequest{
		PublicKey: req.PublicKey,
		Name:      req.Name,
		MeshIP:    req.MeshIP,
		Platform:  req.Platform,
		Available: req.Available,
	})
	if err != nil {
		if writeRegistryAuthError(w, err) {
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device": device})
}

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	availableOnly := r.URL.Query().Get("available") == "1"
	peers, err := s.Registry.Peers(availableOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"peers": peers})
}

func (s *Server) handlePostSignal(w http.ResponseWriter, r *http.Request) {
	var signal coordinator.Signal
	if err := decodeJSON(r, &signal); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.Registry.PostSignal(signal); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) handleTakeSignals(w http.ResponseWriter, r *http.Request) {
	peer := r.URL.Query().Get("peer")
	if peer == "" {
		writeError(w, http.StatusBadRequest, errors.New("peer is required"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"signals": s.Registry.TakeSignals(peer)})
}

func (s *Server) authorized(r *http.Request) bool {
	token, ok := s.bearer(r)
	if !ok {
		return false
	}
	if s.AdminKey != "" && token == s.AdminKey {
		return true
	}
	_, err := s.Registry.Authenticate(token)
	return err == nil
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	token, ok := s.bearer(r)
	if !ok || s.AdminKey == "" || token != s.AdminKey {
		writeError(w, http.StatusUnauthorized, errors.New("admin key required"))
		return false
	}
	return true
}

func (s *Server) bearer(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	value := strings.TrimSpace(header[len(prefix):])
	return value, value != ""
}

func decodeJSON(r *http.Request, value any) error {
	defer r.Body.Close()
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(value); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
