package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"alceacore.ru/alceamesh/internal/audit"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
)

type Reason string

const (
	ReasonMalformed         Reason = "malformed"
	ReasonUnknownDevice     Reason = "unknown_device"
	ReasonRevoked           Reason = "revoked"
	ReasonStale             Reason = "stale"
	ReasonReplay            Reason = "replay"
	ReasonBadSignature      Reason = "bad_signature"
	ReasonSignatureRequired Reason = "signature_required"
)

type deviceContextKey struct{}

func deviceFrom(r *http.Request) *coordinator.Device {
	device, _ := r.Context().Value(deviceContextKey{}).(*coordinator.Device)
	return device
}

func withDevice(r *http.Request, device *coordinator.Device) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), deviceContextKey{}, device))
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) nonces() *identity.NonceCache {
	if s.Nonces == nil {
		s.Nonces = identity.NewNonceCache(identity.DefaultNonceTTL, identity.DefaultNonceLimit)
	}
	return s.Nonces
}

type authError struct {
	Reason  Reason
	Message string
}

func (e *authError) Error() string {
	return string(e.Reason) + ": " + e.Message
}

func newAuthError(reason Reason, format string, args ...any) *authError {
	return &authError{Reason: reason, Message: fmt.Sprintf(format, args...)}
}

func (s *Server) verifySigned(r *http.Request, body []byte) (*coordinator.Device, *authError) {
	signed, err := identity.ParseRequest(r.Header)
	if err != nil {
		return nil, newAuthError(ReasonMalformed, "%v", err)
	}

	device, err := s.Registry.DeviceByID(signed.DeviceID)
	if err != nil {
		if errors.Is(err, coordinator.ErrUnknownDevice) {
			return nil, newAuthError(ReasonUnknownDevice, "device %s is not registered", signed.DeviceID)
		}
		return nil, newAuthError(ReasonUnknownDevice, "%v", err)
	}
	if device.Revoked() {
		return nil, newAuthError(ReasonRevoked, "device %s was revoked", device.ID)
	}
	if device.SignPublicKey == "" {
		return nil, newAuthError(ReasonUnknownDevice, "device %s has no signing key", device.ID)
	}
	if err := signed.Fresh(s.now(), identity.DefaultWindow); err != nil {
		return nil, newAuthError(ReasonStale, "check the device clock: %v", err)
	}
	if s.nonces().Seen(device.ID, signed.Nonce, s.now()) {
		return nil, newAuthError(ReasonReplay, "this request was already used")
	}
	if err := signed.Verify(device.SignPublicKey, r.Method, r.URL.Path, r.URL.RawQuery, body); err != nil {
		return nil, newAuthError(ReasonBadSignature, "%v", err)
	}
	return device, nil
}

func (s *Server) withSignature(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		if s.isAdmin(r) {
			next(w, r)
			return
		}

		switch {
		case identity.HeadersPresent(r.Header):
			device, authErr := s.verifySigned(r, body)
			if authErr != nil {
				s.auditRequest(r, audit.Entry{Event: "auth", Result: "deny", Reason: string(authErr.Reason), Detail: r.URL.Path})
				writeAuthError(w, authErr)
				return
			}
			next(w, withDevice(r, device))
		case s.RequireSignature:
			s.auditRequest(r, audit.Entry{Event: "auth", Result: "deny", Reason: string(ReasonSignatureRequired), Detail: r.URL.Path})
			writeAuthError(w, newAuthError(ReasonSignatureRequired, "this AlceaMesh network requires signed requests"))
		default:
			next(w, r)
		}
	}
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func writeAuthError(w http.ResponseWriter, err *authError) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{
		"error":  err.Message,
		"reason": string(err.Reason),
	})
}

func (s *Server) isAdmin(r *http.Request) bool {
	token, ok := s.bearer(r)
	return ok && s.AdminKey != "" && token == s.AdminKey
}

func writeRegistryAuthError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, coordinator.ErrDeviceRevoked):
		writeAuthError(w, newAuthError(ReasonRevoked, "%v", err))
		return true
	case errors.Is(err, coordinator.ErrUnknownDevice):
		writeAuthError(w, newAuthError(ReasonUnknownDevice, "%v", err))
		return true
	}
	return false
}
