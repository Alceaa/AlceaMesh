package sqlitestore

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"alceacore.ru/alceamesh/internal/coordinator"
)

const schema = `
CREATE TABLE IF NOT EXISTS devices (
	id              TEXT PRIMARY KEY,
	secret          TEXT NOT NULL,
	name            TEXT NOT NULL,
	public_key      TEXT NOT NULL DEFAULT '',
	sign_public_key TEXT NOT NULL DEFAULT '',
	mesh_ip         TEXT NOT NULL DEFAULT '',
	platform        TEXT NOT NULL DEFAULT '',
	available       INTEGER NOT NULL DEFAULT 0,
	enrolled_at     TEXT NOT NULL DEFAULT '',
	last_seen       TEXT NOT NULL DEFAULT '',
	revoked_at      TEXT NOT NULL DEFAULT '',
	legacy          INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_secret ON devices(secret);
CREATE INDEX IF NOT EXISTS idx_devices_public_key ON devices(public_key);

CREATE TABLE IF NOT EXISTS tokens (
	value      TEXT PRIMARY KEY,
	label      TEXT NOT NULL DEFAULT '',
	mesh_ip    TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	expires_at TEXT NOT NULL DEFAULT '',
	used_by    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS invitations (
	id         TEXT PRIMARY KEY,
	code_hash  TEXT NOT NULL,
	label      TEXT NOT NULL DEFAULT '',
	mesh_ip    TEXT NOT NULL DEFAULT '',
	max_uses   INTEGER NOT NULL DEFAULT 0,
	uses       INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT '',
	expires_at TEXT NOT NULL DEFAULT '',
	revoked_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_invitations_code_hash ON invitations(code_hash);
`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("sqlitestore: a database path is required")
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("sqlitestore: cannot create the database folder %s: %w", dir, err)
		}
	}

	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w (check that the folder %s exists and is writable by the service user)", err, dir)
	}
	_, _ = db.Exec(`ALTER TABLE devices ADD COLUMN available INTEGER NOT NULL DEFAULT 0`)
	for _, statement := range []string{
		`ALTER TABLE devices ADD COLUMN sign_public_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE devices ADD COLUMN revoked_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE devices ADD COLUMN legacy INTEGER NOT NULL DEFAULT 0`,
	} {
		_, _ = db.Exec(statement)
	}
	_, _ = db.Exec(`UPDATE devices SET legacy = 1 WHERE legacy = 0 AND sign_public_key = ''`)
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) PutDevice(device coordinator.Device) error {
	_, err := s.db.Exec(`
		INSERT INTO devices (id, secret, name, public_key, sign_public_key, mesh_ip, platform, available, enrolled_at, last_seen, revoked_at, legacy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			secret          = excluded.secret,
			name            = excluded.name,
			public_key      = excluded.public_key,
			sign_public_key = excluded.sign_public_key,
			mesh_ip         = excluded.mesh_ip,
			platform        = excluded.platform,
			available       = excluded.available,
			enrolled_at     = excluded.enrolled_at,
			last_seen       = excluded.last_seen,
			revoked_at      = excluded.revoked_at,
			legacy          = excluded.legacy`,
		device.ID, device.Secret, device.Name, device.PublicKey, device.SignPublicKey,
		device.MeshIP, device.Platform, boolToInt(device.Available),
		formatTime(device.EnrolledAt), formatTime(device.LastSeen),
		formatTime(device.RevokedAt), boolToInt(device.Legacy))
	return err
}

func (s *Store) GetDeviceByPublicKey(publicKey string) (coordinator.Device, bool, error) {
	if publicKey == "" {
		return coordinator.Device{}, false, nil
	}
	row := s.db.QueryRow(selectDevice+` WHERE public_key = ?`, publicKey)
	return scanDevice(row)
}

func (s *Store) GetDevice(id string) (coordinator.Device, bool, error) {
	row := s.db.QueryRow(selectDevice+` WHERE id = ?`, id)
	return scanDevice(row)
}

func (s *Store) GetDeviceBySecret(secret string) (coordinator.Device, bool, error) {
	if secret == "" {
		return coordinator.Device{}, false, nil
	}
	row := s.db.QueryRow(selectDevice+` WHERE secret = ?`, secret)
	return scanDevice(row)
}

func (s *Store) ListDevices() ([]coordinator.Device, error) {
	rows, err := s.db.Query(selectDevice + ` ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []coordinator.Device
	for rows.Next() {
		device, ok, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		if ok {
			devices = append(devices, device)
		}
	}
	return devices, rows.Err()
}

func (s *Store) DeleteDevice(id string) error {
	_, err := s.db.Exec(`DELETE FROM devices WHERE id = ?`, id)
	return err
}

func (s *Store) PutToken(token coordinator.Token) error {
	_, err := s.db.Exec(`
		INSERT INTO tokens (value, label, mesh_ip, created_at, expires_at, used_by)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(value) DO UPDATE SET
			label      = excluded.label,
			mesh_ip    = excluded.mesh_ip,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at,
			used_by    = excluded.used_by`,
		token.Value, token.Label, token.MeshIP,
		formatTime(token.CreatedAt), formatTime(token.ExpiresAt), token.UsedBy)
	return err
}

func (s *Store) GetToken(value string) (coordinator.Token, bool, error) {
	row := s.db.QueryRow(selectToken+` WHERE value = ?`, value)
	return scanToken(row)
}

func (s *Store) ListTokens() ([]coordinator.Token, error) {
	rows, err := s.db.Query(selectToken + ` ORDER BY created_at, value`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []coordinator.Token
	for rows.Next() {
		token, ok, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		if ok {
			tokens = append(tokens, token)
		}
	}
	return tokens, rows.Err()
}

func (s *Store) PutInvitation(invitation coordinator.Invitation) error {
	_, err := s.db.Exec(`
		INSERT INTO invitations (id, code_hash, label, mesh_ip, max_uses, uses, created_at, expires_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			code_hash  = excluded.code_hash,
			label      = excluded.label,
			mesh_ip    = excluded.mesh_ip,
			max_uses   = excluded.max_uses,
			uses       = excluded.uses,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at,
			revoked_at = excluded.revoked_at`,
		invitation.ID, invitation.CodeHash, invitation.Label, invitation.MeshIP,
		invitation.MaxUses, invitation.Uses,
		formatTime(invitation.CreatedAt), formatTime(invitation.ExpiresAt), formatTime(invitation.RevokedAt))
	return err
}

func (s *Store) GetInvitation(id string) (coordinator.Invitation, bool, error) {
	row := s.db.QueryRow(selectInvitation+` WHERE id = ?`, id)
	return scanInvitation(row)
}

func (s *Store) GetInvitationByHash(hash string) (coordinator.Invitation, bool, error) {
	if hash == "" {
		return coordinator.Invitation{}, false, nil
	}
	row := s.db.QueryRow(selectInvitation+` WHERE code_hash = ?`, hash)
	return scanInvitation(row)
}

func (s *Store) ListInvitations() ([]coordinator.Invitation, error) {
	rows, err := s.db.Query(selectInvitation + ` ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invitations []coordinator.Invitation
	for rows.Next() {
		invitation, ok, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		if ok {
			invitations = append(invitations, invitation)
		}
	}
	return invitations, rows.Err()
}

const (
	selectDevice     = `SELECT id, secret, name, public_key, sign_public_key, mesh_ip, platform, available, enrolled_at, last_seen, revoked_at, legacy FROM devices`
	selectToken      = `SELECT value, label, mesh_ip, created_at, expires_at, used_by FROM tokens`
	selectInvitation = `SELECT id, code_hash, label, mesh_ip, max_uses, uses, created_at, expires_at, revoked_at FROM invitations`
)

type scanner interface {
	Scan(dest ...any) error
}

func scanDevice(sc scanner) (coordinator.Device, bool, error) {
	var (
		device     coordinator.Device
		available  int
		legacy     int
		enrolledAt string
		lastSeen   string
		revokedAt  string
	)
	err := sc.Scan(&device.ID, &device.Secret, &device.Name, &device.PublicKey, &device.SignPublicKey,
		&device.MeshIP, &device.Platform, &available, &enrolledAt, &lastSeen, &revokedAt, &legacy)
	if errors.Is(err, sql.ErrNoRows) {
		return coordinator.Device{}, false, nil
	}
	if err != nil {
		return coordinator.Device{}, false, err
	}
	device.Available = available != 0
	device.Legacy = legacy != 0
	device.EnrolledAt = parseTime(enrolledAt)
	device.LastSeen = parseTime(lastSeen)
	device.RevokedAt = parseTime(revokedAt)
	return device, true, nil
}

func scanInvitation(sc scanner) (coordinator.Invitation, bool, error) {
	var (
		invitation coordinator.Invitation
		createdAt  string
		expiresAt  string
		revokedAt  string
	)
	err := sc.Scan(&invitation.ID, &invitation.CodeHash, &invitation.Label, &invitation.MeshIP,
		&invitation.MaxUses, &invitation.Uses, &createdAt, &expiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return coordinator.Invitation{}, false, nil
	}
	if err != nil {
		return coordinator.Invitation{}, false, err
	}
	invitation.CreatedAt = parseTime(createdAt)
	invitation.ExpiresAt = parseTime(expiresAt)
	invitation.RevokedAt = parseTime(revokedAt)
	return invitation, true, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func scanToken(sc scanner) (coordinator.Token, bool, error) {
	var (
		token     coordinator.Token
		createdAt string
		expiresAt string
	)
	err := sc.Scan(&token.Value, &token.Label, &token.MeshIP, &createdAt, &expiresAt, &token.UsedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return coordinator.Token{}, false, nil
	}
	if err != nil {
		return coordinator.Token{}, false, err
	}
	token.CreatedAt = parseTime(createdAt)
	token.ExpiresAt = parseTime(expiresAt)
	return token, true, nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}
