package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

const (
	stunBindingRequest   = 0x0001
	stunBindingSuccess   = 0x0101
	stunHeaderSize       = 20
	stunTransactionSize  = 12
	attrXORMappedAddress = 0x0020
	attrMappedAddress    = 0x0001
)

const stunMagicCookie uint32 = 0x2112A442

var errNoMappedAddress = errors.New("stun: response has no mapped address")

type transactionID [stunTransactionSize]byte

func newTransactionID() (transactionID, error) {
	var id transactionID
	if _, err := rand.Read(id[:]); err != nil {
		return id, err
	}
	return id, nil
}

func buildBindingRequest(id transactionID) []byte {
	request := make([]byte, stunHeaderSize)
	binary.BigEndian.PutUint16(request[0:], stunBindingRequest)
	binary.BigEndian.PutUint16(request[2:], 0)
	binary.BigEndian.PutUint32(request[4:], stunMagicCookie)
	copy(request[8:], id[:])
	return request
}

func isBindingRequest(data []byte) bool {
	if len(data) != stunHeaderSize {
		return false
	}
	return binary.BigEndian.Uint16(data[0:]) == stunBindingRequest &&
		binary.BigEndian.Uint32(data[4:]) == stunMagicCookie
}

func bindingRequestID(data []byte) (transactionID, bool) {
	var id transactionID
	if !isBindingRequest(data) {
		return id, false
	}
	copy(id[:], data[8:stunHeaderSize])
	return id, true
}

func stunResponseID(data []byte) (transactionID, bool) {
	var id transactionID
	if len(data) < stunHeaderSize {
		return id, false
	}
	if binary.BigEndian.Uint16(data[0:]) != stunBindingSuccess {
		return id, false
	}
	if binary.BigEndian.Uint32(data[4:]) != stunMagicCookie {
		return id, false
	}
	copy(id[:], data[8:stunHeaderSize])
	return id, true
}

func buildBindingResponse(id transactionID, addr netip.AddrPort) []byte {
	value := encodeXORMappedAddress(addr, id)
	attribute := make([]byte, 4+len(value))
	binary.BigEndian.PutUint16(attribute[0:], attrXORMappedAddress)
	binary.BigEndian.PutUint16(attribute[2:], uint16(len(value)))
	copy(attribute[4:], value)

	response := make([]byte, stunHeaderSize+len(attribute))
	binary.BigEndian.PutUint16(response[0:], stunBindingSuccess)
	binary.BigEndian.PutUint16(response[2:], uint16(len(attribute)))
	binary.BigEndian.PutUint32(response[4:], stunMagicCookie)
	copy(response[8:], id[:])
	copy(response[stunHeaderSize:], attribute)
	return response
}

func parseBindingResponse(data []byte, id transactionID) (netip.AddrPort, error) {
	if len(data) < stunHeaderSize {
		return netip.AddrPort{}, errors.New("stun: short message")
	}
	if binary.BigEndian.Uint32(data[4:]) != stunMagicCookie {
		return netip.AddrPort{}, errors.New("stun: bad magic cookie")
	}
	if binary.BigEndian.Uint16(data[0:]) != stunBindingSuccess {
		return netip.AddrPort{}, errors.New("stun: not a binding success response")
	}
	for i := range id {
		if data[8+i] != id[i] {
			return netip.AddrPort{}, errors.New("stun: transaction mismatch")
		}
	}

	body := data[stunHeaderSize:]
	length := int(binary.BigEndian.Uint16(data[2:]))
	if length > len(body) {
		length = len(body)
	}

	var fallback netip.AddrPort
	for offset := 0; offset+4 <= length; {
		kind := binary.BigEndian.Uint16(body[offset:])
		size := int(binary.BigEndian.Uint16(body[offset+2:]))
		start := offset + 4
		if start+size > length {
			break
		}
		value := body[start : start+size]

		switch kind {
		case attrXORMappedAddress:
			if addr, err := decodeXORMappedAddress(value, id); err == nil {
				return addr, nil
			}
		case attrMappedAddress:
			if addr, err := decodeMappedAddress(value); err == nil {
				fallback = addr
			}
		}

		offset = start + size + (4-size%4)%4
	}
	if fallback.IsValid() {
		return fallback, nil
	}
	return netip.AddrPort{}, errNoMappedAddress
}

func encodeXORMappedAddress(addr netip.AddrPort, id transactionID) []byte {
	if addr.Addr().Is4() {
		value := make([]byte, 8)
		value[1] = 0x01
		binary.BigEndian.PutUint16(value[2:], addr.Port()^uint16(stunMagicCookie>>16))
		ip := addr.Addr().As4()
		for i := 0; i < 4; i++ {
			value[4+i] = ip[i] ^ byte(stunMagicCookie>>(24-8*i))
		}
		return value
	}

	value := make([]byte, 20)
	value[1] = 0x02
	binary.BigEndian.PutUint16(value[2:], addr.Port()^uint16(stunMagicCookie>>16))
	ip := addr.Addr().As16()
	for i := 0; i < 16; i++ {
		if i < 4 {
			value[4+i] = ip[i] ^ byte(stunMagicCookie>>(24-8*i))
			continue
		}
		value[4+i] = ip[i] ^ id[i-4]
	}
	return value
}

func decodeXORMappedAddress(value []byte, id transactionID) (netip.AddrPort, error) {
	if len(value) < 4 {
		return netip.AddrPort{}, errors.New("stun: short xor-mapped address")
	}
	port := binary.BigEndian.Uint16(value[2:]) ^ uint16(stunMagicCookie>>16)

	switch value[1] {
	case 0x01:
		if len(value) < 8 {
			return netip.AddrPort{}, errors.New("stun: short ipv4 xor-mapped address")
		}
		var ip [4]byte
		for i := 0; i < 4; i++ {
			ip[i] = value[4+i] ^ byte(stunMagicCookie>>(24-8*i))
		}
		return netip.AddrPortFrom(netip.AddrFrom4(ip), port), nil

	case 0x02:
		if len(value) < 20 {
			return netip.AddrPort{}, errors.New("stun: short ipv6 xor-mapped address")
		}
		var ip [16]byte
		for i := 0; i < 16; i++ {
			if i < 4 {
				ip[i] = value[4+i] ^ byte(stunMagicCookie>>(24-8*i))
				continue
			}
			ip[i] = value[4+i] ^ id[i-4]
		}
		return netip.AddrPortFrom(netip.AddrFrom16(ip), port), nil

	default:
		return netip.AddrPort{}, errors.New("stun: unsupported address family")
	}
}

func decodeMappedAddress(value []byte) (netip.AddrPort, error) {
	if len(value) < 8 {
		return netip.AddrPort{}, errors.New("stun: short mapped address")
	}
	port := binary.BigEndian.Uint16(value[2:])
	switch value[1] {
	case 0x01:
		var ip [4]byte
		copy(ip[:], value[4:8])
		return netip.AddrPortFrom(netip.AddrFrom4(ip), port), nil
	case 0x02:
		if len(value) < 20 {
			return netip.AddrPort{}, errors.New("stun: short ipv6 mapped address")
		}
		var ip [16]byte
		copy(ip[:], value[4:20])
		return netip.AddrPortFrom(netip.AddrFrom16(ip), port), nil
	default:
		return netip.AddrPort{}, errors.New("stun: unsupported address family")
	}
}

func resolveUDP(server string) (*net.UDPAddr, error) {
	addr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return nil, fmt.Errorf("stun: resolve %s: %w", server, err)
	}
	return addr, nil
}

const stunAttemptInterval = 400 * time.Millisecond
