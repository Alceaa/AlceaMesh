package version

import (
	"regexp"
	"testing"
)

func TestVersionLooksLikeARelease(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
	pattern := regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)
	if !pattern.MatchString(Version) {
		t.Errorf("Version %q must look like 1.0.2 or 0.8.0-m8", Version)
	}
	if String() != Version {
		t.Errorf("String() = %q, want %q", String(), Version)
	}
}
