package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Notes   string `json:"notes,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

func IsNewer(current, candidate string) bool {
	return Compare(current, candidate) < 0
}

func Compare(a, b string) int {
	coreA, suffixA := splitVersion(a)
	coreB, suffixB := splitVersion(b)

	if result := compareCore(coreA, coreB); result != 0 {
		return result
	}
	return compareSuffix(suffixA, suffixB)
}

func splitVersion(version string) ([]int, string) {
	version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	core := version
	suffix := ""
	if index := strings.IndexAny(version, "-+"); index >= 0 {
		core = version[:index]
		suffix = version[index+1:]
	}

	parts := strings.Split(core, ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			numbers = append(numbers, 0)
			continue
		}
		numbers = append(numbers, number)
	}
	return numbers, suffix
}

func compareCore(a, b []int) int {
	length := len(a)
	if len(b) > length {
		length = len(b)
	}
	for index := 0; index < length; index++ {
		valueA, valueB := 0, 0
		if index < len(a) {
			valueA = a[index]
		}
		if index < len(b) {
			valueB = b[index]
		}
		if valueA != valueB {
			if valueA < valueB {
				return -1
			}
			return 1
		}
	}
	return 0
}

func compareSuffix(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	if numberA, numberB, ok := milestoneNumbers(a, b); ok {
		switch {
		case numberA < numberB:
			return -1
		case numberA > numberB:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

func milestoneNumbers(a, b string) (int, int, bool) {
	first, okFirst := milestoneNumber(a)
	second, okSecond := milestoneNumber(b)
	if !okFirst || !okSecond {
		return 0, 0, false
	}
	return first, second, true
}

func milestoneNumber(value string) (int, bool) {
	if !strings.HasPrefix(value, "m") {
		return 0, false
	}
	number, err := strconv.Atoi(strings.TrimPrefix(value, "m"))
	if err != nil {
		return 0, false
	}
	return number, true
}

func Fetch(ctx context.Context, client *http.Client, manifestURL string) (Release, error) {
	if strings.TrimSpace(manifestURL) == "" {
		return Release{}, errors.New("update: manifest URL is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return Release{}, fmt.Errorf("update: request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("update: fetch manifest: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("update: manifest status %s", response.Status)
	}

	var release Release
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("update: decode manifest: %w", err)
	}
	if err := release.validate(); err != nil {
		return Release{}, err
	}
	return release, nil
}

func (r Release) validate() error {
	if strings.TrimSpace(r.Version) == "" {
		return errors.New("update: manifest has no version")
	}
	if !strings.HasPrefix(strings.ToLower(r.URL), "https://") && !strings.HasPrefix(strings.ToLower(r.URL), "http://") {
		return errors.New("update: manifest URL must be http(s)")
	}
	if len(strings.TrimSpace(r.SHA256)) != 64 {
		return errors.New("update: manifest must contain a sha256 checksum")
	}
	if _, err := hex.DecodeString(r.SHA256); err != nil {
		return errors.New("update: manifest sha256 is not hex")
	}
	return nil
}

func Download(ctx context.Context, client *http.Client, release Release, dir string) (string, error) {
	if err := release.validate(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.URL, nil)
	if err != nil {
		return "", fmt.Errorf("update: request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("update: download: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update: download status %s", response.Status)
	}

	target := filepath.Join(dir, "AlceaMesh-Setup-"+release.Version+".exe")
	partial := target + ".part"
	file, err := os.Create(partial)
	if err != nil {
		return "", err
	}

	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(partial)
		return "", fmt.Errorf("update: save installer: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(partial)
		return "", closeErr
	}

	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, release.SHA256) {
		_ = os.Remove(partial)
		return "", fmt.Errorf("update: checksum mismatch (got %s, want %s)", got, release.SHA256)
	}
	if err := os.Rename(partial, target); err != nil {
		_ = os.Remove(partial)
		return "", err
	}
	return target, nil
}
