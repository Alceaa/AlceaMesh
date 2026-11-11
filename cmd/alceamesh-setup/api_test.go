package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"alceacore.ru/alceamesh/internal/setup"
)

func newTestState(t *testing.T, mode string) *state {
	t.Helper()

	payload := fstest.MapFS{
		"alceamesh.exe":  {Data: []byte("app")},
		"alceameshd.exe": {Data: []byte("daemon")},
		"wintun.dll":     {Data: []byte("dll")},
	}
	return &state{
		payload: payload,
		mode:    mode,
		options: setup.Options{
			Dir:       filepath.Join(t.TempDir(), "AlceaMesh"),
			Version:   "0.8.0-test",
			Service:   true,
			Firewall:  true,
			Shortcuts: true,
		},
	}
}

func request(t *testing.T, s *state, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	s.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return data
}

func TestStateEndpointDescribesThePlan(t *testing.T) {
	s := newTestState(t, "install")

	recorder := request(t, s, "/api/state")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}

	data := decode(t, recorder)
	if data["mode"] != "install" {
		t.Errorf("mode = %v", data["mode"])
	}
	if data["version"] != appVersion {
		t.Errorf("version = %v, want %v", data["version"], appVersion)
	}
	if data["elevated"] != false {
		t.Errorf("elevated = %v, want false in tests", data["elevated"])
	}
	if plan, ok := data["plan"].([]any); !ok || len(plan) == 0 {
		t.Errorf("plan = %v", data["plan"])
	}
	payload, ok := data["payload"].([]any)
	if !ok || len(payload) != len(setup.PayloadFiles()) {
		t.Errorf("payload = %v, want %d entries", data["payload"], len(setup.PayloadFiles()))
	}
}

func TestUninstallModePlanMentionsService(t *testing.T) {
	s := newTestState(t, "uninstall")
	s.options.Purge = true

	data := decode(t, request(t, s, "/api/state"))
	if data["mode"] != "uninstall" {
		t.Errorf("mode = %v", data["mode"])
	}
	plan := data["plan"].([]any)
	joined := ""
	for _, line := range plan {
		joined += line.(string) + "\n"
	}
	for _, want := range []string{"службу", "настройки"} {
		if !strings.Contains(joined, want) {
			t.Errorf("plan is missing %q: %s", want, joined)
		}
	}
}

func TestInstallDryRunThroughAPI(t *testing.T) {
	s := newTestState(t, "install")
	dir := s.options.Dir

	query := url.Values{
		"dir":       {dir},
		"autostart": {"1"},
		"shortcuts": {"1"},
		"service":   {"1"},
		"firewall":  {"1"},
		"dryrun":    {"1"},
	}
	started := decode(t, request(t, s, "/api/start?"+query.Encode()))
	if started["started"] != true {
		t.Fatalf("start = %v", started)
	}

	var log map[string]any
	for attempt := 0; attempt < 60; attempt++ {
		log = decode(t, request(t, s, "/api/log"))
		if log["running"] == false {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if log["running"] != false {
		t.Fatal("install did not finish")
	}
	if log["done"] != true {
		t.Fatalf("done = %v, error = %v", log["done"], log["error"])
	}
	lines, _ := log["lines"].([]any)
	if len(lines) == 0 {
		t.Fatal("the log must describe the steps")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("a dry run must not create %s", dir)
	}
	if s.options.Dir != dir {
		t.Errorf("the chosen folder must be remembered, got %q", s.options.Dir)
	}
}

func TestInstallRejectsInvalidFolder(t *testing.T) {
	s := newTestState(t, "install")

	for _, dir := range []string{"", "not-absolute", `C:\`} {
		result := decode(t, request(t, s, "/api/start?dir="+url.QueryEscape(dir)+"&dryrun=1"))
		if result["started"] == true {
			t.Errorf("dir %q must be rejected", dir)
		}
		if message, _ := result["error"].(string); message == "" {
			t.Errorf("dir %q: no error message", dir)
		}
	}
}

func TestInstallRequiresAdministrator(t *testing.T) {
	if setup.Elevated() {
		t.Skip("the test must run without administrator rights")
	}
	s := newTestState(t, "install")

	result := decode(t, request(t, s, "/api/start?dir="+url.QueryEscape(s.options.Dir)))
	if result["started"] == true {
		t.Fatal("a real install must not start without administrator rights")
	}
	if message, _ := result["error"].(string); !strings.Contains(message, "администратора") {
		t.Errorf("error = %v", result["error"])
	}
}

func TestAssetsServeThePage(t *testing.T) {
	s := newTestState(t, "install")

	recorder := request(t, s, "/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Установка AlceaMesh") {
		t.Error("the page must be served from the embedded assets")
	}
	if !strings.Contains(recorder.Body.String(), "/api/state") {
		t.Error("the page must talk to the API")
	}

	if got := request(t, s, "/nope.txt").Code; got != http.StatusNotFound {
		t.Errorf("status for a missing asset = %d, want 404", got)
	}
}

func TestLaunchNeedsAnInstalledApp(t *testing.T) {
	s := newTestState(t, "install")

	result := decode(t, request(t, s, "/api/launch"))
	if _, ok := result["error"]; !ok {
		t.Errorf("launching a missing app must report an error, got %v", result)
	}
}

func TestApplyQueryOverridesOptions(t *testing.T) {
	base := setup.Options{Dir: `C:\a`, Autostart: false, Shortcuts: true, Service: true, Firewall: true}

	got := applyQuery(base, url.Values{
		"dir":       {`D:\b`},
		"autostart": {"1"},
		"shortcuts": {"0"},
		"dryrun":    {"true"},
	})
	if got.Dir != `D:\b` || !got.Autostart || got.Shortcuts || !got.DryRun {
		t.Errorf("applyQuery = %+v", got)
	}
	if !got.Service || !got.Firewall {
		t.Error("untouched flags must keep their values")
	}

	kept := applyQuery(base, url.Values{})
	if kept != base {
		t.Errorf("an empty query must change nothing, got %+v", kept)
	}
}
