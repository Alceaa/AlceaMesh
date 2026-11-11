package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		current   string
		candidate string
		want      int
	}{
		{"0.8.0-m8", "1.0.0", -1},
		{"1.0.0", "0.8.0-m8", 1},
		{"1.0.0", "1.0.0", 0},
		{"v1.2.3", "1.2.3", 0},
		{"1.2", "1.2.0", 0},
		{"1.0.0-m8", "1.0.0", -1},
		{"1.0.0-m10", "1.0.0-m9", 1},
		{"0.9.0", "1.0.0-m1", -1},
		{"1.0.1", "1.0.0", 1},
	}
	for _, item := range cases {
		if got := Compare(item.current, item.candidate); got != item.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", item.current, item.candidate, got, item.want)
		}
	}

	if !IsNewer("0.8.0-m8", "0.9.0") {
		t.Error("0.9.0 must be newer than 0.8.0-m8")
	}
	if IsNewer("1.0.0", "1.0.0") {
		t.Error("the same version is not newer")
	}
}

func TestFetchReadsManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.0.0","url":"` + "http://example.com/setup.exe" + `","sha256":"` + repeat("a", 64) + `","notes":"первый релиз"}`))
	}))
	defer server.Close()

	release, err := Fetch(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if release.Version != "1.0.0" || release.Notes != "первый релиз" {
		t.Errorf("release = %+v", release)
	}
}

func TestFetchRejectsBadManifests(t *testing.T) {
	cases := map[string]string{
		"no version":  `{"url":"http://example.com/setup.exe","sha256":"` + repeat("a", 64) + `"}`,
		"no url":      `{"version":"1.0.0","sha256":"` + repeat("a", 64) + `"}`,
		"short hash":  `{"version":"1.0.0","url":"http://example.com/setup.exe","sha256":"abc"}`,
		"broken json": `{"version":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			if _, err := Fetch(context.Background(), server.Client(), server.URL); err == nil {
				t.Errorf("manifest %q must be rejected", body)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer server.Close()
	if _, err := Fetch(context.Background(), server.Client(), server.URL); err == nil {
		t.Error("a 404 manifest must fail")
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	payload := []byte("installer payload")
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	release := Release{
		Version: "1.0.0",
		URL:     server.URL,
		SHA256:  hex.EncodeToString(sum[:]),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	path, err := Download(ctx, server.Client(), release, dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != string(payload) {
		t.Errorf("content = %q, want %q", data, payload)
	}
	if filepath.Base(path) != "AlceaMesh-Setup-1.0.0.exe" {
		t.Errorf("file name = %q", filepath.Base(path))
	}

	release.SHA256 = repeat("b", 64)
	if _, err := Download(ctx, server.Client(), release, dir); err == nil {
		t.Fatal("a checksum mismatch must fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("the broken download must be removed, %d entries left", len(entries))
	}
}

func repeat(value string, count int) string {
	result := ""
	for index := 0; index < count; index++ {
		result += value
	}
	return result
}
