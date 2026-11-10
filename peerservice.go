package main

import (
	"context"
	"errors"
	"sync"

	"alceacore.ru/alceamesh/internal/coordclient"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/p2p"
	"alceacore.ru/alceamesh/internal/settings"
)

type PeerService struct {
	mu       sync.Mutex
	agent    *p2p.Agent
	agentKey string

	transfers map[string]*peerTransfer
	states    map[string]TransferProgress
	order     []string
	seq       int
}

func NewPeerService() *PeerService {
	return &PeerService{
		transfers: make(map[string]*peerTransfer),
		states:    make(map[string]TransferProgress),
		order:     make([]string, 0, 8),
	}
}

type PeerStatus struct {
	Peer     string `json:"peer"`
	Name     string `json:"name"`
	MeshIP   string `json:"meshIP"`
	State    string `json:"state"`
	Endpoint string `json:"endpoint"`
	RTTms    int    `json:"rttMs"`
	Message  string `json:"message"`
}

type NetworkTransport struct {
	PublicEndpoint string             `json:"publicEndpoint"`
	Direct         bool               `json:"direct"`
	Inbox          string             `json:"inbox"`
	Message        string             `json:"message"`
	Peers          []PeerStatus       `json:"peers"`
	Transfers      []TransferProgress `json:"transfers"`
}

func (s *PeerService) Transport() NetworkTransport {
	agent, err := s.agentFor()
	if err != nil {
		return NetworkTransport{Message: friendlyText(err, fallbackPeer)}
	}

	transport := NetworkTransport{
		Inbox:     s.inboxDir(),
		Transfers: s.transferStates(),
	}
	if public := agent.PublicAddr(); public.IsValid() {
		transport.PublicEndpoint = public.String()
	}
	for _, status := range agent.Statuses() {
		transport.Peers = append(transport.Peers, PeerStatus{
			Peer:     status.Peer,
			Name:     status.Name,
			MeshIP:   status.MeshIP,
			State:    string(status.State),
			Endpoint: status.Endpoint,
			RTTms:    status.RTTms,
			Message:  status.Message,
		})
		if status.State == p2p.StateDirect {
			transport.Direct = true
			s.ensureTransfer(status.Peer, status.Name)
		}
	}
	return transport
}

func (s *PeerService) Connect(peerKey, name, meshIP string) error {
	if peerKey == "" {
		return errors.New("Не удалось определить устройство. Обновите список устройств.")
	}
	agent, err := s.agentFor()
	if err != nil {
		return err
	}
	return friendlyError(agent.Connect(p2p.Peer{PublicKey: peerKey, Name: name, MeshIP: meshIP}), fallbackPeer)
}

func (s *PeerService) Disconnect(peerKey string) error {
	s.mu.Lock()
	agent := s.agent
	s.mu.Unlock()

	if agent == nil {
		return nil
	}
	agent.Disconnect(peerKey)
	return nil
}

func (s *PeerService) Shutdown() {
	s.stopTransfers()

	s.mu.Lock()
	agent := s.agent
	s.agent = nil
	s.agentKey = ""
	s.mu.Unlock()

	if agent != nil {
		agent.Close()
	}
}

func (s *PeerService) agentFor() (*p2p.Agent, error) {
	stored, err := settings.Load()
	if err != nil {
		return nil, err
	}

	identity := identityOf(stored)
	if identity.PublicKey == "" {
		return nil, errors.New("Импортируйте конфигурацию, чтобы включить прямые соединения.")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.agent != nil && s.agentKey == identity.PublicKey {
		return s.agent, nil
	}
	if s.agent != nil {
		s.agent.Close()
		s.agent = nil
	}

	client := coordclient.New(coordinatorURL(), "")
	agent := p2p.NewAgent(p2p.Config{
		PublicKey: identity.PublicKey,
		Name:      identity.Name,
		MeshIP:    identity.MeshIP,
		Signaler:  signalAdapter{client: client, me: identity.PublicKey},
	})
	if err := agent.Start(); err != nil {
		return nil, friendlyError(err, fallbackPeer)
	}

	s.agent = agent
	s.agentKey = identity.PublicKey
	return agent, nil
}

type signalAdapter struct {
	client *coordclient.Client
	me     string
}

func (a signalAdapter) PostSignal(_ context.Context, to string, signal p2p.Signal) error {
	return a.client.PostSignal(context.Background(), coordinator.Signal{
		From:       a.me,
		To:         to,
		Kind:       signal.Kind,
		Token:      signal.Token,
		Candidates: signal.Candidates,
		MeshIP:     signal.MeshIP,
		Name:       signal.Name,
	})
}

func (a signalAdapter) TakeSignals(_ context.Context) ([]p2p.Signal, error) {
	signals, err := a.client.Signals(context.Background(), a.me)
	if err != nil {
		return nil, err
	}
	out := make([]p2p.Signal, 0, len(signals))
	for _, signal := range signals {
		out = append(out, p2p.Signal{
			From:       signal.From,
			Kind:       signal.Kind,
			Token:      signal.Token,
			Candidates: signal.Candidates,
			MeshIP:     signal.MeshIP,
			Name:       signal.Name,
		})
	}
	return out, nil
}
