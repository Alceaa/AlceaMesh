//go:build windows

package ipc

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const socketDirACL = "D:(A;OICI;GA;;;SY)(A;OICI;GA;;;BA)(A;OICI;0x1301BF;;;AU)"

func SocketDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "AlceaMesh")
}

func prepareSocketDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(socketDirACL)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
}
