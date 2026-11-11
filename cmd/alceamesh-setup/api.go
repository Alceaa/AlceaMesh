package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"alceacore.ru/alceamesh/internal/setup"
)

type state struct {
	payload fs.FS
	mode    string

	mu      sync.Mutex
	options setup.Options
	lines   []string
	running bool
	done    bool
	errText string
}

func (s *state) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/state":
		s.handleState(w, r)
	case "/api/choose-dir":
		s.handleChooseDir(w)
	case "/api/start":
		s.handleStart(w, r)
	case "/api/log":
		s.handleLog(w)
	case "/api/elevate":
		s.handleElevate(w)
	case "/api/launch":
		s.handleLaunch(w)
	case "/api/close":
		writeJSON(w, map[string]any{"ok": true})
		if appInstance != nil {
			appInstance.Quit()
		}
	default:
		s.serveAsset(w, r)
	}
}

func (s *state) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := "web/index.html"
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		name = "web/" + strings.TrimPrefix(r.URL.Path, "/")
	}

	data, err := webAssets.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	_, _ = w.Write(data)
}

func (s *state) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	options := applyQuery(s.options, r.URL.Query())
	s.mu.Unlock()

	plan := setup.Plan(options)
	if s.mode == "uninstall" {
		plan = []string{
			"Остановить и удалить службу AlceaMesh",
			"Убрать правила брандмауэра и ярлыки",
			"Удалить файлы программы",
		}
		if options.Purge {
			plan = append(plan, "Удалить настройки и импортированный .conf")
		}
	}

	payload := make([]map[string]any, 0, 3)
	for _, name := range setup.PayloadFiles() {
		info, err := fs.Stat(s.payload, name)
		if err != nil || info.IsDir() {
			continue
		}
		payload = append(payload, map[string]any{"name": name, "size": info.Size()})
	}

	installed := false
	if info, err := os.Stat(filepath.Join(options.Dir, "alceamesh.exe")); err == nil && !info.IsDir() {
		installed = true
	}

	writeJSON(w, map[string]any{
		"version":   appVersion,
		"mode":      s.mode,
		"dir":       options.Dir,
		"elevated":  setup.Elevated(),
		"installed": installed,
		"payload":   payload,
		"plan":      plan,
		"autostart": options.Autostart,
		"shortcuts": options.Shortcuts,
		"purge":     options.Purge,
	})
}

func (s *state) handleLog(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()

	lines := make([]string, len(s.lines))
	copy(lines, s.lines)
	writeJSON(w, map[string]any{
		"lines":   lines,
		"running": s.running,
		"done":    s.done,
		"error":   s.errText,
	})
}

func (s *state) handleChooseDir(w http.ResponseWriter) {
	if appInstance == nil {
		writeJSON(w, map[string]any{"error": "Окно недоступно"})
		return
	}
	dir, err := appInstance.Dialog.OpenFile().
		SetTitle("Куда установить AlceaMesh").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"dir": dir})
}

func (s *state) handleElevate(w http.ResponseWriter) {
	if err := setup.Elevate(commandLine(os.Args[1:])); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
	if appInstance != nil {
		go appInstance.Quit()
	}
}

func (s *state) handleLaunch(w http.ResponseWriter) {
	s.mu.Lock()
	dir := s.options.Dir
	s.mu.Unlock()

	exe := filepath.Join(dir, "alceamesh.exe")
	if _, err := os.Stat(exe); err != nil {
		writeJSON(w, map[string]any{"error": "Не нахожу " + exe})
		return
	}
	if err := exec.Command(exe).Start(); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func applyQuery(options setup.Options, query url.Values) setup.Options {
	if values, ok := query["dir"]; ok && len(values) > 0 {
		options.Dir = strings.TrimSpace(values[0])
	}
	targets := map[string]*bool{
		"autostart": &options.Autostart,
		"shortcuts": &options.Shortcuts,
		"service":   &options.Service,
		"firewall":  &options.Firewall,
		"purge":     &options.Purge,
		"dryrun":    &options.DryRun,
	}
	for name, target := range targets {
		if value := query.Get(name); value != "" {
			*target = value == "1" || strings.EqualFold(value, "true")
		}
	}
	return options
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	data, err := json.Marshal(value)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"не удалось собрать ответ"}`))
		return
	}
	_, _ = w.Write(data)
}

func (s *state) handleStart(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		writeJSON(w, map[string]any{"started": false, "error": "Уже выполняется."})
		return
	}
	mode := s.mode
	options := applyQuery(s.options, r.URL.Query())
	s.mu.Unlock()

	if mode == "install" {
		if err := setup.Validate(options.Dir); err != nil {
			writeJSON(w, map[string]any{"started": false, "error": err.Error()})
			return
		}
		if !options.DryRun && !setup.Elevated() {
			writeJSON(w, map[string]any{"started": false, "error": "Для установки нужны права администратора."})
			return
		}
	}

	self, _ := os.Executable()
	payload := s.payload

	s.mu.Lock()
	s.options = options
	s.lines = nil
	s.running = true
	s.done = false
	s.errText = ""
	s.mu.Unlock()

	logger := func(line string) {
		s.mu.Lock()
		s.lines = append(s.lines, line)
		s.mu.Unlock()
	}

	go func() {
		var err error
		if mode == "uninstall" {
			err = setup.Uninstall(options, self, logger)
		} else {
			err = setup.Install(payload, self, options, logger)
		}
		s.mu.Lock()
		s.running = false
		s.done = err == nil
		if err != nil {
			s.errText = err.Error()
		}
		s.mu.Unlock()
	}()

	writeJSON(w, map[string]any{"started": true})
}
