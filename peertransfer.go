package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"alceacore.ru/alceamesh/internal/settings"
	"alceacore.ru/alceamesh/internal/transfer"
	"alceacore.ru/alceamesh/internal/wgconf"
)

type TransferProgress struct {
	Key       string `json:"key"`
	Direction string `json:"direction"`
	Peer      string `json:"peer"`
	PeerName  string `json:"peerName"`
	Name      string `json:"name"`
	Rel       string `json:"relative"`
	Path      string `json:"path"`
	Sent      int64  `json:"sent"`
	Total     int64  `json:"total"`
	Files     int    `json:"files"`
	Index     int    `json:"index"`
	Queued    bool   `json:"queued"`
	Resumed   bool   `json:"resumed"`
	Done      bool   `json:"done"`
	Error     string `json:"error"`
}

type sendTask struct {
	key  string
	path string
}

type peerTransfer struct {
	session *transfer.Session
	packets net.PacketConn
	ctx     context.Context
	cancel  context.CancelFunc

	mu     sync.Mutex
	tasks  []sendTask
	active bool
}

func (s *PeerService) inboxDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "AlceaMesh")
	}
	return filepath.Join(home, "Downloads", "AlceaMesh")
}

func (s *PeerService) transferStates() []TransferProgress {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]TransferProgress, 0, len(s.order))
	for _, key := range s.order {
		if state, ok := s.states[key]; ok {
			out = append(out, state)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return !finishedState(out[i]) && finishedState(out[j])
	})
	return out
}

func finishedState(state TransferProgress) bool {
	return state.Done || state.Error != ""
}

func (s *PeerService) setTransferState(key string, state TransferProgress) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.states[key]; !exists {
		s.order = append(s.order, key)
	}
	state.Key = key
	s.states[key] = state
}

func (s *PeerService) nextTransferKey(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return fmt.Sprintf("%s#%d", prefix, s.seq)
}

func (s *PeerService) ClearFinished() {
	s.mu.Lock()
	defer s.mu.Unlock()

	remaining := make([]string, 0, len(s.order))
	for _, key := range s.order {
		state, ok := s.states[key]
		if !ok {
			continue
		}
		if finishedState(state) {
			delete(s.states, key)
			continue
		}
		remaining = append(remaining, key)
	}
	s.order = remaining
}

func (s *PeerService) ensureTransfer(peerKey, name string) {
	if peerKey == "" {
		return
	}

	s.mu.Lock()
	_, running := s.transfers[peerKey]
	agent := s.agent
	s.mu.Unlock()
	if running || agent == nil {
		return
	}

	stored, err := settings.Load()
	if err != nil {
		return
	}
	privateKey := localPrivateKey(stored)
	if privateKey == "" {
		return
	}

	secret, err := transfer.SharedSecret(privateKey, peerKey)
	if err != nil {
		return
	}
	cert, err := transfer.Certificate(secret)
	if err != nil {
		return
	}
	packets, err := agent.DataConn(peerKey)
	if err != nil {
		return
	}
	session, err := transfer.NewSession(packets, cert)
	if err != nil {
		_ = packets.Close()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	record := &peerTransfer{session: session, packets: packets, ctx: ctx, cancel: cancel}

	s.mu.Lock()
	if _, exists := s.transfers[peerKey]; exists {
		s.mu.Unlock()
		cancel()
		_ = session.Close()
		return
	}
	s.transfers[peerKey] = record
	s.mu.Unlock()

	go s.receiveLoop(ctx, peerKey, name, session)
}

func (s *PeerService) receiveLoop(ctx context.Context, peerKey, name string, session *transfer.Session) {
	inbox := s.inboxDir()

	for ctx.Err() == nil {
		key := s.nextTransferKey("receive:" + peerKey)
		progress, err := session.Receive(ctx, inbox, func(p transfer.Progress) {
			s.setTransferState(key, TransferProgress{
				Direction: "receive",
				Peer:      peerKey,
				PeerName:  name,
				Name:      p.Name,
				Rel:       p.Rel,
				Sent:      p.Sent,
				Total:     p.Total,
				Files:     p.Files,
				Index:     p.Index,
				Resumed:   p.Resumed,
			})
		})

		state := TransferProgress{
			Direction: "receive",
			Peer:      peerKey,
			PeerName:  name,
			Name:      progress.Name,
			Rel:       progress.Rel,
			Path:      progress.Path,
			Sent:      progress.Sent,
			Total:     progress.Total,
			Files:     progress.Files,
			Index:     progress.Index,
			Resumed:   progress.Resumed,
			Done:      err == nil,
		}
		if state.Name == "" {
			state.Name = name
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			state.Error = friendlyText(err, fallbackTransfer)
		}
		s.setTransferState(key, state)

		if err != nil {
			s.dropTransfer(peerKey)
			return
		}
	}
}

func (s *PeerService) SendFiles(peerKey string) (TransferProgress, error) {
	return s.pick(peerKey, false)
}

func (s *PeerService) SendFolder(peerKey string) (TransferProgress, error) {
	return s.pick(peerKey, true)
}

func (s *PeerService) pick(peerKey string, folder bool) (TransferProgress, error) {
	if peerKey == "" {
		return TransferProgress{}, errors.New("Устройство не выбрано.")
	}
	if appInstance == nil {
		return TransferProgress{}, errors.New(fallbackWindow)
	}

	picker := appInstance.Dialog.OpenFile()
	if folder {
		picker = picker.SetTitle("Выберите папку для отправки").CanChooseFiles(false).CanChooseDirectories(true)
		path, err := picker.PromptForSingleSelection()
		if err != nil {
			return TransferProgress{}, friendlyError(err, fallbackDialog)
		}
		if path == "" {
			return TransferProgress{}, nil
		}
		return s.enqueue(peerKey, []string{path})
	}

	picker = picker.SetTitle("Выберите файлы для отправки").AddFilter("Все файлы", "*")
	paths, err := picker.PromptForMultipleSelection()
	if err != nil {
		return TransferProgress{}, friendlyError(err, fallbackDialog)
	}
	if len(paths) == 0 {
		return TransferProgress{}, nil
	}
	return s.enqueue(peerKey, paths)
}

func (s *PeerService) enqueue(peerKey string, paths []string) (TransferProgress, error) {
	if peerKey == "" {
		return TransferProgress{}, errors.New("Устройство не выбрано.")
	}
	if len(paths) == 0 {
		return TransferProgress{}, nil
	}

	s.mu.Lock()
	record := s.transfers[peerKey]
	s.mu.Unlock()
	if record == nil {
		return TransferProgress{}, errors.New("Прямое соединение с устройством не установлено.")
	}

	peerName := s.peerName(peerKey)
	tasks := make([]sendTask, 0, len(paths))
	for _, path := range paths {
		key := s.nextTransferKey("send:" + peerKey)
		s.setTransferState(key, TransferProgress{
			Direction: "send",
			Peer:      peerKey,
			PeerName:  peerName,
			Name:      filepath.Base(path),
			Path:      path,
			Queued:    true,
		})
		tasks = append(tasks, sendTask{key: key, path: path})
	}

	record.mu.Lock()
	record.tasks = append(record.tasks, tasks...)
	start := !record.active
	record.active = true
	record.mu.Unlock()

	if start {
		go s.sendWorker(record, peerKey, peerName)
	}

	return TransferProgress{
		Direction: "send",
		Peer:      peerKey,
		PeerName:  peerName,
		Name:      filepath.Base(paths[0]),
		Path:      paths[0],
		Files:     len(paths),
		Queued:    true,
	}, nil
}

func (s *PeerService) sendWorker(record *peerTransfer, peerKey, peerName string) {
	for {
		record.mu.Lock()
		if len(record.tasks) == 0 {
			record.active = false
			record.mu.Unlock()
			return
		}
		task := record.tasks[0]
		record.tasks = record.tasks[1:]
		record.mu.Unlock()

		s.runSend(record, peerKey, peerName, task)

		if record.ctx.Err() != nil {
			record.mu.Lock()
			pending := record.tasks
			record.tasks = nil
			record.active = false
			record.mu.Unlock()

			for _, left := range pending {
				s.setTransferState(left.key, TransferProgress{
					Direction: "send",
					Peer:      peerKey,
					PeerName:  peerName,
					Name:      filepath.Base(left.path),
					Path:      left.path,
					Done:      true,
					Error:     "Передача отменена.",
				})
			}
			return
		}
	}
}

func (s *PeerService) runSend(record *peerTransfer, peerKey, peerName string, task sendTask) {
	state, sendErr := record.session.Send(record.ctx, s.peerAddr(peerKey), task.path, func(p transfer.Progress) {
		s.setTransferState(task.key, TransferProgress{
			Direction: "send",
			Peer:      peerKey,
			PeerName:  peerName,
			Name:      p.Name,
			Rel:       p.Rel,
			Path:      task.path,
			Sent:      p.Sent,
			Total:     p.Total,
			Files:     p.Files,
			Index:     p.Index,
			Resumed:   p.Resumed,
		})
	})

	final := TransferProgress{
		Direction: "send",
		Peer:      peerKey,
		PeerName:  peerName,
		Name:      state.Name,
		Rel:       state.Rel,
		Path:      task.path,
		Sent:      state.Sent,
		Total:     state.Total,
		Files:     state.Files,
		Index:     state.Index,
		Resumed:   state.Resumed,
		Done:      sendErr == nil,
	}
	if final.Name == "" {
		final.Name = filepath.Base(task.path)
	}
	if sendErr != nil {
		final.Error = friendlyText(sendErr, fallbackTransfer)
	}
	s.setTransferState(task.key, final)
}

func (s *PeerService) dropTransfer(peerKey string) {
	s.mu.Lock()
	record := s.transfers[peerKey]
	delete(s.transfers, peerKey)
	s.mu.Unlock()

	if record != nil {
		record.cancel()
		_ = record.session.Close()
		_ = record.packets.Close()
	}
}

func (s *PeerService) stopTransfers() {
	s.mu.Lock()
	records := make([]*peerTransfer, 0, len(s.transfers))
	for key, record := range s.transfers {
		records = append(records, record)
		delete(s.transfers, key)
	}
	s.states = make(map[string]TransferProgress)
	s.order = nil
	s.seq = 0
	s.mu.Unlock()

	for _, record := range records {
		record.cancel()
		_ = record.session.Close()
		_ = record.packets.Close()
	}
}

func (s *PeerService) peerAddr(peerKey string) net.Addr {
	s.mu.Lock()
	agent := s.agent
	s.mu.Unlock()

	if agent != nil {
		for _, status := range agent.Statuses() {
			if status.Peer != peerKey {
				continue
			}
			if addr, err := netip.ParseAddrPort(status.Endpoint); err == nil {
				return net.UDPAddrFromAddrPort(addr)
			}
		}
	}
	return &net.UDPAddr{}
}

func (s *PeerService) peerName(peerKey string) string {
	s.mu.Lock()
	agent := s.agent
	s.mu.Unlock()

	if agent != nil {
		for _, status := range agent.Statuses() {
			if status.Peer == peerKey && status.Name != "" {
				return status.Name
			}
		}
	}
	return ""
}

func localPrivateKey(stored *settings.Settings) string {
	if stored.ConfigText == "" {
		return ""
	}
	config, err := wgconf.Parse([]byte(stored.ConfigText))
	if err != nil {
		return ""
	}
	return config.Interface.PrivateKey
}
