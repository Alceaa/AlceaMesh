package api

import (
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"alceacore.ru/alceamesh/internal/audit"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
)

const (
	claimTTL      = 5 * time.Minute
	claimBoxLimit = 256
)

const (
	ReasonUnknownClaim  Reason = "unknown_claim"
	ReasonClaimExpired  Reason = "claim_expired"
	ReasonBadProof      Reason = "bad_proof"
	ReasonBadSignKey    Reason = "bad_sign_key"
	ReasonNotAttachable Reason = "not_attachable"
)

type pendingClaim struct {
	deviceID  string
	wgKey     string
	ephemeral *ecdh.PrivateKey
	challenge []byte
	expiresAt time.Time
}

type claimStore struct {
	mu     sync.Mutex
	claims map[string]pendingClaim
}

func newClaimStore() *claimStore {
	return &claimStore{claims: make(map[string]pendingClaim)}
}

func (c *claimStore) put(id string, claim pendingClaim) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, value := range c.claims {
		if now.After(value.expiresAt) {
			delete(c.claims, key)
		}
	}
	if len(c.claims) >= claimBoxLimit {
		oldestID := ""
		var oldest time.Time
		for key, value := range c.claims {
			if oldestID == "" || value.expiresAt.Before(oldest) {
				oldestID, oldest = key, value.expiresAt
			}
		}
		delete(c.claims, oldestID)
	}
	c.claims[id] = claim
}

func (c *claimStore) take(id string, now time.Time) (pendingClaim, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	claim, ok := c.claims[id]
	if !ok {
		return pendingClaim{}, false
	}
	delete(c.claims, id)
	return claim, true
}

func (s *Server) claims() *claimStore {
	if s.claimStore == nil {
		s.claimStore = newClaimStore()
	}
	return s.claimStore
}

type claimChallengeRequest struct {
	PublicKey string `json:"public_key"`
}

type claimChallengeResponse struct {
	ClaimID   string `json:"claim_id"`
	Challenge string `json:"challenge"`
	ServerKey string `json:"server_key"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Server) handleClaimChallenge(w http.ResponseWriter, r *http.Request) {
	var req claimChallengeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	device, err := s.Registry.DeviceByWGKey(req.PublicKey)
	if err != nil {
		if errors.Is(err, coordinator.ErrUnknownDevice) {
			writeAuthError(w, newAuthError(ReasonUnknownDevice, "this WireGuard key is not in the registry"))
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if device.Revoked() {
		writeAuthError(w, newAuthError(ReasonRevoked, "device %s was revoked", device.ID))
		return
	}
	if !device.Attachable() {
		writeAuthError(w, newAuthError(ReasonNotAttachable, "device %s does not need attaching", device.ID))
		return
	}

	ephemeral, err := identity.NewEphemeralKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	challenge, err := identity.NewChallenge()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	claimID, err := identity.NewNonce()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	expiresAt := s.now().Add(claimTTL)
	s.claims().put(claimID, pendingClaim{
		deviceID:  device.ID,
		wgKey:     device.PublicKey,
		ephemeral: ephemeral,
		challenge: challenge,
		expiresAt: expiresAt,
	})
	s.auditRequest(r, audit.Entry{Event: "claim", Result: "challenge", DeviceID: device.ID, DeviceName: device.Name})

	writeJSON(w, http.StatusOK, claimChallengeResponse{
		ClaimID:   claimID,
		Challenge: identity.EncodeToken(challenge),
		ServerKey: identity.X25519PublicKey(ephemeral),
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	})
}

type claimRequest struct {
	ClaimID       string `json:"claim_id"`
	Proof         string `json:"proof"`
	SignPublicKey string `json:"sign_public_key"`
	Signature     string `json:"signature"`
}

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req claimRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}

	claim, ok := s.claims().take(req.ClaimID, s.now())
	if !ok {
		s.auditRequest(r, audit.Entry{Event: "claim", Result: "fail", Reason: string(ReasonUnknownClaim)})
		writeAuthError(w, newAuthError(ReasonUnknownClaim, "this claim is unknown or already used"))
		return
	}
	if s.now().After(claim.expiresAt) {
		s.auditRequest(r, audit.Entry{Event: "claim", Result: "fail", Reason: string(ReasonClaimExpired), DeviceID: claim.deviceID})
		writeAuthError(w, newAuthError(ReasonClaimExpired, "ask for a new challenge"))
		return
	}

	device, err := s.Registry.DeviceByID(claim.deviceID)
	if err != nil {
		writeAuthError(w, newAuthError(ReasonUnknownDevice, "device %s is gone", claim.deviceID))
		return
	}
	if device.Revoked() {
		writeAuthError(w, newAuthError(ReasonRevoked, "device %s was revoked", device.ID))
		return
	}
	if !device.Attachable() {
		writeAuthError(w, newAuthError(ReasonNotAttachable, "device %s does not need attaching", device.ID))
		return
	}

	proof, err := identity.DecodeToken(req.Proof)
	if err != nil {
		writeAuthError(w, newAuthError(ReasonMalformed, "%v", err))
		return
	}
	shared, err := identity.SharedSecret(claim.ephemeral, claim.wgKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !identity.ProofMatches(identity.Proof(shared, claim.challenge), proof) {
		writeAuthError(w, newAuthError(ReasonBadProof, "the WireGuard key was not proven"))
		return
	}

	if err := identity.VerifySignature(req.SignPublicKey, identity.CanonicalClaim(req.ClaimID, req.Proof, req.SignPublicKey), req.Signature); err != nil {
		writeAuthError(w, newAuthError(ReasonBadSignKey, "%v", err))
		return
	}

	attached, err := s.Registry.AttachDevice(device.ID, req.SignPublicKey)
	if err != nil {
		switch {
		case errors.Is(err, coordinator.ErrDeviceRevoked):
			s.auditRequest(r, audit.Entry{Event: "claim", Result: "fail", Reason: string(ReasonRevoked), DeviceID: device.ID})
			writeAuthError(w, newAuthError(ReasonRevoked, "%v", err))
		default:
			s.auditRequest(r, audit.Entry{Event: "claim", Result: "fail", Reason: string(ReasonNotAttachable), DeviceID: device.ID})
			writeAuthError(w, newAuthError(ReasonNotAttachable, "%v", err))
		}
		return
	}
	s.auditRequest(r, audit.Entry{Event: "claim", Result: "ok", DeviceID: attached.ID, DeviceName: attached.Name})
	writeJSON(w, http.StatusOK, map[string]any{"device": attached})
}
