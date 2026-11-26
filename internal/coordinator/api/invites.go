package api

import (
	"errors"
	"net/http"
	"time"

	"alceacore.ru/alceamesh/internal/audit"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
)

const (
	ReasonInviteUnknown   Reason = "unknown_invite"
	ReasonInviteExpired   Reason = "invite_expired"
	ReasonInviteRevoked   Reason = "invite_revoked"
	ReasonInviteExhausted Reason = "invite_exhausted"
)

func inviteReason(err error) Reason {
	switch {
	case errors.Is(err, coordinator.ErrInviteRevoked):
		return ReasonInviteRevoked
	case errors.Is(err, coordinator.ErrInviteExpired):
		return ReasonInviteExpired
	case errors.Is(err, coordinator.ErrInviteExhausted):
		return ReasonInviteExhausted
	default:
		return ReasonInviteUnknown
	}
}

type createInviteRequest struct {
	Label      string `json:"label"`
	MeshIP     string `json:"mesh_ip"`
	Uses       int    `json:"uses"`
	TTLSeconds int    `json:"ttl_seconds"`
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req createInviteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var ttl time.Duration
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	invitation, code, err := s.Registry.CreateInvite(req.Label, req.MeshIP, req.Uses, ttl)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.auditRequest(r, audit.Entry{Event: "invite", Result: "created", InviteID: invitation.ID, Detail: invitation.Label})
	writeJSON(w, http.StatusCreated, map[string]any{
		"invitation": invitation,
		"code":       code,
	})
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	invitations, err := s.Registry.Invites()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": invitations})
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	invitation, err := s.Registry.RevokeInvite(r.PathValue("id"))
	if err != nil {
		writeAuthError(w, newAuthError(inviteReason(err), "%v", err))
		return
	}
	s.auditRequest(r, audit.Entry{Event: "invite", Result: "revoked", InviteID: invitation.ID})
	writeJSON(w, http.StatusOK, map[string]any{"invitation": invitation})
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	device, err := s.Registry.RevokeDevice(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	s.auditRequest(r, audit.Entry{Event: "device", Result: "revoked", DeviceID: device.ID, DeviceName: device.Name})
	writeJSON(w, http.StatusOK, map[string]any{"device": device})
}

type inviteEnrollRequest struct {
	Invite        string `json:"invite"`
	Name          string `json:"name"`
	Platform      string `json:"platform"`
	PublicKey     string `json:"public_key"`
	SignPublicKey string `json:"sign_public_key"`
	Signature     string `json:"signature"`
}

func (s *Server) handleInviteEnroll(w http.ResponseWriter, r *http.Request, req inviteEnrollRequest) {
	payload := identity.CanonicalEnroll(req.Invite, req.Name, req.Platform, req.PublicKey, req.SignPublicKey)
	if err := identity.VerifySignature(req.SignPublicKey, payload, req.Signature); err != nil {
		s.auditRequest(r, audit.Entry{Event: "enroll", Result: "fail", Reason: string(ReasonBadSignKey), DeviceName: req.Name})
		writeAuthError(w, newAuthError(ReasonBadSignKey, "%v", err))
		return
	}

	device, err := s.Registry.EnrollWithInvite(coordinator.InviteEnrollRequest{
		Code:          req.Invite,
		Name:          req.Name,
		PublicKey:     req.PublicKey,
		SignPublicKey: req.SignPublicKey,
		Platform:      req.Platform,
	})
	if err != nil {
		if errors.Is(err, coordinator.ErrInviteUnknown) || errors.Is(err, coordinator.ErrInviteExpired) ||
			errors.Is(err, coordinator.ErrInviteRevoked) || errors.Is(err, coordinator.ErrInviteExhausted) {
			reason := inviteReason(err)
			s.auditRequest(r, audit.Entry{Event: "enroll", Result: "fail", Reason: string(reason), DeviceName: req.Name})
			writeAuthError(w, newAuthError(reason, "%v", err))
			return
		}
		s.auditRequest(r, audit.Entry{Event: "enroll", Result: "fail", Reason: "rejected", DeviceName: req.Name})
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.auditRequest(r, audit.Entry{
		Event:      "enroll",
		Result:     "ok",
		DeviceID:   device.ID,
		DeviceName: device.Name,
		Detail:     "invite",
	})
	writeJSON(w, http.StatusCreated, enrollResponse{Device: *device, Secret: device.Secret})
}
