package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"

	"alceacore.ru/alceamesh/internal/settings"
	"alceacore.ru/alceamesh/internal/transfer"
	"alceacore.ru/alceamesh/internal/wgconf"
)

type TransferProgress struct {
	Direction string `json:"direction"`
	Peer      string `json:"peer"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Sent      int64  `json:"sent"`
	Total     int64  `json:"total"`
	Resumed   bool   `json:"resumed"`
	Done      bool   `json:"done"`
	Error     string `json:"error"`
}

type peerTransfer struct {
	session *transfer.Session
	packets net.PacketConn
	cancel  context.CancelFunc
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

	out := make([]TransferProgress, 0, len(s.states))
	for _, state := range s.states {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Done != out[j].Done {
			return !out[i].Done
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *PeerService) setTransfer(state TransferProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.Peer] = state
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
	record := &peerTransfer{session: session, packets: packets, cancel: cancel}

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
		progress, err := session.Receive(ctx, inbox, func(p transfer.Progress) {
			s.setTransfer(TransferProgress{
				Direction: "receive",
				Peer:      peerKey,
				Name:      p.Name,
				Sent:      p.Sent,
				Total:     p.Total,
				Resumed:   p.Resumed,
			})
		})

		state := TransferProgress{
			Direction: "receive",
			Peer:      peerKey,
			Name:      progress.Name,
			Path:      progress.Path,
			Sent:      progress.Sent,
			Total:     progress.Total,
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
		s.setTransfer(state)

		if err != nil {
			s.dropTransfer(peerKey)
			return
		}
	}
}

func (s *PeerService) SendFile(peerKey string) (TransferProgress, error) {
	if peerKey == "" {
		return TransferProgress{}, errors.New("Устройство не выбрано.")
	}
	if appInstance == nil {
		return TransferProgress{}, errors.New(fallbackWindow)
	}

	path, err := appInstance.Dialog.OpenFile().
		SetTitle("Выберите файл для отправки").
		AddFilter("Все файлы", "*").
		PromptForSingleSelection()
	if err != nil {
		return TransferProgress{}, friendlyError(err, fallbackDialog)
	}
	if path == "" {
		return TransferProgress{}, nil
	}

	s.mu.Lock()
	record := s.transfers[peerKey]
	s.mu.Unlock()
	if record == nil {
		return TransferProgress{}, errors.New("Прямое соединение с устройством не установлено.")
	}

	state, sendErr := record.session.Send(context.Background(), s.peerAddr(peerKey), path, func(p transfer.Progress) {
		s.setTransfer(TransferProgress{
			Direction: "send",
			Peer:      peerKey,
			Name:      p.Name,
			Sent:      p.Sent,
			Total:     p.Total,
			Resumed:   p.Resumed,
		})
	})

	final := TransferProgress{
		Direction: "send",
		Peer:      peerKey,
		Name:      state.Name,
		Path:      path,
		Sent:      state.Sent,
		Total:     state.Total,
		Resumed:   state.Resumed,
		Done:      sendErr == nil,
	}
	if sendErr != nil {
		final.Error = friendlyText(sendErr, fallbackTransfer)
	}
	s.setTransfer(final)
	if sendErr != nil {
		return final, friendlyError(sendErr, fallbackTransfer)
	}
	return final, nil
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
