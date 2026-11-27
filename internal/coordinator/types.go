package coordinator

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

type Device struct {
	ID            string    `json:"id"`
	Secret        string    `json:"-"`
	Name          string    `json:"name"`
	PublicKey     string    `json:"public_key,omitempty"`
	SignPublicKey string    `json:"sign_public_key,omitempty"`
	MeshIP        string    `json:"mesh_ip,omitempty"`
	Platform      string    `json:"platform,omitempty"`
	Available     bool      `json:"available"`
	EnrolledAt    time.Time `json:"enrolled_at"`
	LastSeen      time.Time `json:"last_seen"`
	RevokedAt     time.Time `json:"revoked_at,omitempty"`
	Legacy        bool      `json:"legacy,omitempty"`
}

type DeviceView struct {
	Device
	Online bool `json:"online"`
}

func (d Device) Revoked() bool {
	return !d.RevokedAt.IsZero()
}

func (d Device) Attachable() bool {
	return !d.Revoked() && strings.TrimSpace(d.SignPublicKey) == "" && strings.TrimSpace(d.PublicKey) != ""
}

type AnnounceRequest struct {
	PublicKey string
	Name      string
	MeshIP    string
	Platform  string
	Available bool
}

type Signal struct {
	From       string    `json:"from"`
	To         string    `json:"to"`
	Kind       string    `json:"kind"`
	Token      string    `json:"token"`
	Candidates []string  `json:"candidates,omitempty"`
	MeshIP     string    `json:"mesh_ip,omitempty"`
	Name       string    `json:"name,omitempty"`
	At         time.Time `json:"at"`
}

type Token struct {
	Value     string    `json:"value"`
	Label     string    `json:"label,omitempty"`
	MeshIP    string    `json:"mesh_ip,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	UsedBy    string    `json:"used_by,omitempty"`
}

type EnrollRequest struct {
	Token         string
	Name          string
	PublicKey     string
	SignPublicKey string
	Platform      string
}

type InviteEnrollRequest struct {
	Code          string
	Name          string
	PublicKey     string
	SignPublicKey string
	Platform      string
}

type Invitation struct {
	ID        string    `json:"id"`
	CodeHash  string    `json:"-"`
	Label     string    `json:"label,omitempty"`
	MeshIP    string    `json:"mesh_ip,omitempty"`
	MaxUses   int       `json:"max_uses"`
	Uses      int       `json:"uses"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
}

func (i Invitation) Expired(now time.Time) bool {
	return !i.ExpiresAt.IsZero() && now.After(i.ExpiresAt)
}

func (i Invitation) Revoked() bool {
	return !i.RevokedAt.IsZero()
}

func (i Invitation) Exhausted() bool {
	return i.MaxUses > 0 && i.Uses >= i.MaxUses
}

func (i Invitation) Usable(now time.Time) bool {
	return !i.Revoked() && !i.Expired(now) && !i.Exhausted()
}

func (t Token) Expired(now time.Time) bool {
	return !t.ExpiresAt.IsZero() && now.After(t.ExpiresAt)
}

func (t Token) Used() bool {
	return t.UsedBy != ""
}

func HashInviteCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}
