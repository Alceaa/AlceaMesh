//go:build windows

package identity

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cryptProtectUIForbidden = 0x1

func protect(seed []byte) ([]byte, string, error) {
	blob, err := protectDPAPI(seed)
	if err != nil {
		return append([]byte(nil), seed...), SchemePlain, nil
	}
	return blob, SchemeDPAPI, nil
}

func unprotect(blob []byte) ([]byte, error) {
	out, err := unprotectDPAPI(blob)
	if err != nil {
		return nil, fmt.Errorf("identity: не удалось расшифровать ключ через DPAPI: %w", err)
	}
	return out, nil
}

func restrict(string) error {
	return nil
}

func protectDPAPI(data []byte) ([]byte, error) {
	return dpapiCrypt(data, true)
}

func unprotectDPAPI(data []byte) ([]byte, error) {
	return dpapiCrypt(data, false)
}

func dpapiCrypt(data []byte, encrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("identity: пустые данные")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob

	var err error
	if encrypt {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, cryptProtectUIForbidden, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, cryptProtectUIForbidden, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))

	result := make([]byte, out.Size)
	copy(result, unsafe.Slice(out.Data, out.Size))
	return result, nil
}
