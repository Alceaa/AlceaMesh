package wgconf

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseFull(t *testing.T) {
	cfg, err := ParseFile("testdata/full.conf")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if got, want := cfg.Interface.PrivateKey, "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="; got != want {
		t.Errorf("PrivateKey = %q, want %q", got, want)
	}
	if got, want := cfg.Interface.Address, []string{"10.0.0.2/24", "fd86:ea04:1115::2/64"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Address = %v, want %v", got, want)
	}
	if got, want := cfg.Interface.DNS, []string{"10.0.0.1", "1.1.1.1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DNS = %v, want %v", got, want)
	}
	if got, want := cfg.Interface.MTU, 1420; got != want {
		t.Errorf("MTU = %d, want %d", got, want)
	}
	if got, want := cfg.Interface.ListenPort, 51820; got != want {
		t.Errorf("ListenPort = %d, want %d", got, want)
	}
	if got, want := cfg.Interface.Table, "auto"; got != want {
		t.Errorf("Table = %q, want %q", got, want)
	}
	if !cfg.Interface.SaveConfig {
		t.Errorf("SaveConfig = false, want true")
	}
	if got, want := len(cfg.Peers), 2; got != want {
		t.Fatalf("len(Peers) = %d, want %d", got, want)
	}

	p0 := cfg.Peers[0]
	if got, want := p0.PublicKey, "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="; got != want {
		t.Errorf("peer0 PublicKey = %q, want %q", got, want)
	}
	if got, want := p0.PresharedKey, "FpCyhws9cxwWoV4xELtfJvjJN+zQVRPISllRWgeopVE="; got != want {
		t.Errorf("peer0 PresharedKey = %q, want %q", got, want)
	}
	if got, want := p0.AllowedIPs, []string{"10.0.0.0/24", "fd86:ea04:1115::/64"}; !reflect.DeepEqual(got, want) {
		t.Errorf("peer0 AllowedIPs = %v, want %v", got, want)
	}
	if got, want := p0.Endpoint, "vps.alceacore.ru:51820"; got != want {
		t.Errorf("peer0 Endpoint = %q, want %q", got, want)
	}
	if got, want := p0.PersistentKeepalive, 25; got != want {
		t.Errorf("peer0 PersistentKeepalive = %d, want %d", got, want)
	}

	if got, want := cfg.Peers[1].AllowedIPs, []string{"10.0.0.3/32"}; !reflect.DeepEqual(got, want) {
		t.Errorf("peer1 AllowedIPs = %v, want %v", got, want)
	}
}

func TestParseMinimal(t *testing.T) {
	cfg, err := ParseFile("testdata/minimal.conf")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got, want := len(cfg.Peers), 1; got != want {
		t.Fatalf("len(Peers) = %d, want %d", got, want)
	}
	if got, want := cfg.Peers[0].AllowedIPs, []string{"0.0.0.0/0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AllowedIPs = %v, want %v", got, want)
	}
	if got, want := cfg.Interface.ListenPort, 0; got != want {
		t.Errorf("ListenPort = %d, want %d", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	orig, err := ParseFile("testdata/full.conf")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	again, err := Parse(orig.Marshal())
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}
	if !reflect.DeepEqual(orig, again) {
		t.Errorf("round trip mismatch:\norig:  %+v\nagain: %+v", orig, again)
	}
}

func TestToUAPI(t *testing.T) {
	cfg, err := ParseFile("testdata/full.conf")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	uapi, err := cfg.ToUAPI()
	if err != nil {
		t.Fatalf("ToUAPI: %v", err)
	}
	want := []string{
		"private_key=c809f3e5317e9575c9b5ed78b638b7ce530dabe85ddab614220241801ddf0669",
		"listen_port=51820",
		"replace_peers=true",
		"public_key=c53201039adba14be71f886da1d8dbe9eebded08cb111b75340078999aa9f038",
		"preshared_key=1690b2870b3d731c16a15e3110bb5f26f8c937ecd05513c84a59515a07a8a551",
		"endpoint=vps.alceacore.ru:51820",
		"persistent_keepalive_interval=25",
		"replace_allowed_ips=true",
		"allowed_ip=10.0.0.0/24",
		"allowed_ip=fd86:ea04:1115::/64",
		"public_key=1c8828f7137324c58b2804928624ea2326f1674537c062e251e2753ca7fcca4c",
		"replace_allowed_ips=true",
		"allowed_ip=10.0.0.3/32",
	}
	got := strings.Split(strings.TrimSpace(uapi), "\n")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToUAPI mismatch:\ngot:  %v\nwant: %v", got, want)
	}
}

func TestParseErrors(t *testing.T) {
	priv := "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	pub := "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
	cases := map[string]string{
		"missing private key": "[Interface]\nAddress = 10.0.0.2/24\n[Peer]\nPublicKey = " + pub + "\n",
		"bad base64":          "[Interface]\nPrivateKey = not_base64!!\n",
		"short key":           "[Interface]\nPrivateKey = QUFBQQ==\n",
		"unknown section":     "[Nope]\nPrivateKey = " + priv + "\n",
		"missing equals":      "[Interface]\nPrivateKey\n",
		"unknown interface":   "[Interface]\nPrivateKey = " + priv + "\nBogus = 1\n",
		"missing public key":  "[Interface]\nPrivateKey = " + priv + "\n[Peer]\nAllowedIPs = 10.0.0.0/24\n",
		"bad allowed ip":      "[Interface]\nPrivateKey = " + priv + "\n[Peer]\nPublicKey = " + pub + "\nAllowedIPs = 10.0.0.0\n",
		"key outside section": "PrivateKey = " + priv + "\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(in)); err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}
