package naming

import (
	"path/filepath"
	"strings"
)

func TunnelFromPath(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.Map(sanitize, base)
	if base == "" {
		return "AlceaMesh"
	}
	return base
}

func sanitize(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		return r
	default:
		return '-'
	}
}
