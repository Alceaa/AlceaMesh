package p2p

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	packetMagic   = "ALCP"
	packetVersion = 1
	tokenSize     = 16
	packetSize    = 4 + 1 + 1 + tokenSize
)

type packetKind byte

const (
	packetPunch packetKind = 1
	packetAck   packetKind = 2
	packetBye   packetKind = 3
)

type token [tokenSize]byte

type packet struct {
	kind  packetKind
	token token
}

func newToken() (token, error) {
	var value token
	if _, err := rand.Read(value[:]); err != nil {
		return value, err
	}
	return value, nil
}

func encodePacket(p packet) []byte {
	data := make([]byte, packetSize)
	copy(data, packetMagic)
	data[4] = packetVersion
	data[5] = byte(p.kind)
	copy(data[6:], p.token[:])
	return data
}

func decodePacket(data []byte) (packet, error) {
	if len(data) != packetSize {
		return packet{}, errors.New("p2p: unexpected packet size")
	}
	if string(data[:4]) != packetMagic || data[4] != packetVersion {
		return packet{}, errors.New("p2p: not a p2p packet")
	}
	var p packet
	p.kind = packetKind(data[5])
	copy(p.token[:], data[6:])
	return p, nil
}

func isP2PPacket(data []byte) bool {
	_, err := decodePacket(data)
	return err == nil
}

func (t token) String() string {
	return hex.EncodeToString(t[:])
}

func parseToken(value string) (token, error) {
	var t token
	raw, err := hex.DecodeString(value)
	if err != nil {
		return t, fmt.Errorf("p2p: invalid token: %w", err)
	}
	if len(raw) != tokenSize {
		return t, fmt.Errorf("p2p: token must be %d bytes", tokenSize)
	}
	copy(t[:], raw)
	return t, nil
}
