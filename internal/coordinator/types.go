package coordinator

import "time"

type Device struct {
	ID         string    `json:"id"`
	Secret     string    `json:"-"`
	Name       string    `json:"name"`
	PublicKey  string    `json:"public_key,omitempty"`
	MeshIP     string    `json:"mesh_ip,omitempty"`
	Platform   string    `json:"platform,omitempty"`
	EnrolledAt time.Time `json:"enrolled_at"`
	LastSeen   time.Time `json:"last_seen"`
}

type DeviceView struct {
	Device
	Online bool `json:"online"`
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
	Token     string
	Name      string
	PublicKey string
	Platform  string
}

func (t Token) Expired(now time.Time) bool {
	return !t.ExpiresAt.IsZero() && now.After(t.ExpiresAt)
}

func (t Token) Used() bool {
	return t.UsedBy != ""
}
