package p2p

import (
	"net/netip"
	"testing"
)

func TestXORMappedAddressRoundTrip(t *testing.T) {
	id, err := newTransactionID()
	if err != nil {
		t.Fatalf("transaction id: %v", err)
	}

	addresses := []netip.AddrPort{
		netip.MustParseAddrPort("203.0.113.7:51820"),
		netip.MustParseAddrPort("127.0.0.1:1"),
		netip.MustParseAddrPort("[2001:db8::1]:443"),
	}
	for _, want := range addresses {
		value := encodeXORMappedAddress(want, id)
		got, err := decodeXORMappedAddress(value, id)
		if err != nil {
			t.Fatalf("decode %s: %v", want, err)
		}
		if got != want {
			t.Errorf("round trip = %s, want %s", got, want)
		}
	}
}

func TestBindingResponseRoundTrip(t *testing.T) {
	id, err := newTransactionID()
	if err != nil {
		t.Fatalf("transaction id: %v", err)
	}
	want := netip.MustParseAddrPort("198.51.100.10:34567")

	response := buildBindingResponse(id, want)
	got, err := parseBindingResponse(response, id)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != want {
		t.Errorf("parse = %s, want %s", got, want)
	}
}

func TestBindingResponseRejectsAlienTransaction(t *testing.T) {
	id, _ := newTransactionID()
	other, _ := newTransactionID()

	response := buildBindingResponse(id, netip.MustParseAddrPort("198.51.100.10:34567"))
	if _, err := parseBindingResponse(response, other); err == nil {
		t.Fatal("expected an error for a mismatched transaction id")
	}
}

func TestBindingRequestShape(t *testing.T) {
	id, _ := newTransactionID()
	request := buildBindingRequest(id)

	if len(request) != stunHeaderSize {
		t.Fatalf("request size = %d, want %d", len(request), stunHeaderSize)
	}
	if !isBindingRequest(request) {
		t.Fatal("request is not recognised as a binding request")
	}
	gotID, ok := bindingRequestID(request)
	if !ok || gotID != id {
		t.Fatalf("transaction id mismatch: %v %v", gotID, id)
	}
}

func TestParseBindingResponseWithoutMappedAddress(t *testing.T) {
	id, _ := newTransactionID()
	response := buildBindingResponse(id, netip.MustParseAddrPort("198.51.100.10:34567"))
	response[2] = 0
	response[3] = 0

	if _, err := parseBindingResponse(response, id); err == nil {
		t.Fatal("expected an error when no mapped address is present")
	}
}

func TestParseTokenRoundTrip(t *testing.T) {
	original, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	parsed, err := parseToken(original.String())
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	if parsed != original {
		t.Errorf("token mismatch")
	}
	if _, err := parseToken("zz"); err == nil {
		t.Error("expected an error for an invalid token")
	}
}
