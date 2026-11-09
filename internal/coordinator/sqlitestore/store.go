package sqlitestore

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"alceacore.ru/alceamesh/internal/coordinator"
)

const schema = `
CREATE TABLE IF NOT EXISTS devices (
	id          TEXT PRIMARY KEY,
	secret      TEXT NOT NULL,
	name        TEXT NOT NULL,
	public_key  TEXT NOT NULL DEFAULT '',
	mesh_ip     TEXT NOT NULL DEFAULT '',
	platform    TEXT NOT NULL DEFAULT '',
	available   INTEGER NOT NULL DEFAULT 0,
	enrolled_at TEXT NOT NULL DEFAULT '',
	last_seen   TEXT NOT NULL DEFAULT ''
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
`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	_, _ = db.Exec(`ALTER TABLE devices ADD COLUMN available INTEGER NOT NULL DEFAULT 0`)
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) PutDevice(device coordinator.Device) error {
	_, err := s.db.Exec(`
		INSERT INTO devices (id, secret, name, public_key, mesh_ip, platform, available, enrolled_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			secret      = excluded.secret,
			name        = excluded.name,
			public_key  = excluded.public_key,
			mesh_ip     = excluded.mesh_ip,
			platform    = excluded.platform,
			available   = excluded.available,
			enrolled_at = excluded.enrolled_at,
			last_seen   = excluded.last_seen`,
		device.ID, device.Secret, device.Name, device.PublicKey,
		device.MeshIP, device.Platform, boolToInt(device.Available),
		formatTime(device.EnrolledAt), formatTime(device.LastSeen))
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

const (
	selectDevice = `SELECT id, secret, name, public_key, mesh_ip, platform, available, enrolled_at, last_seen FROM devices`
	selectToken  = `SELECT value, label, mesh_ip, created_at, expires_at, used_by FROM tokens`
)

type scanner interface {
	Scan(dest ...any) error
}

func scanDevice(sc scanner) (coordinator.Device, bool, error) {
	var (
		device     coordinator.Device
		available  int
		enrolledAt string
		lastSeen   string
	)
	err := sc.Scan(&device.ID, &device.Secret, &device.Name, &device.PublicKey,
		&device.MeshIP, &device.Platform, &available, &enrolledAt, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return coordinator.Device{}, false, nil
	}
	if err != nil {
		return coordinator.Device{}, false, err
	}
	device.Available = available != 0
	device.EnrolledAt = parseTime(enrolledAt)
	device.LastSeen = parseTime(lastSeen)
	return device, true, nil
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
