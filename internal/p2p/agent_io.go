package p2p

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

func (a *Agent) readLoop() {
	defer a.wg.Done()
	buf := make([]byte, 2048)
	for {
		n, from, err := a.conn.ReadFromUDP(buf)
		if err != nil {
			if a.isClosed() {
				return
			}
			continue
		}
		a.dispatch(buf[:n], from)
	}
}

func (a *Agent) dispatch(data []byte, from *net.UDPAddr) {
	if id, ok := bindingRequestID(data); ok {
		if addr, err := netip.ParseAddrPort(from.String()); err == nil {
			_, _ = a.conn.WriteToUDP(buildBindingResponse(id, addr), from)
		}
		return
	}

	if isP2PPacket(data) {
		a.handleP2PPacket(data, from)
		return
	}

	if id, ok := stunResponseID(data); ok {
		a.deliverStunResponse(id, data)
	}
}

func (a *Agent) handleP2PPacket(data []byte, from *net.UDPAddr) {
	addr, err := netip.ParseAddrPort(from.String())
	if err != nil {
		return
	}
	for _, s := range a.snapshot() {
		if reply := s.handle(data, addr); reply != nil {
			_, _ = a.conn.WriteToUDP(reply, from)
			return
		}
	}
}

func (a *Agent) deliverStunResponse(id transactionID, data []byte) {
	a.mu.Lock()
	waiter := a.waiting[id]
	delete(a.waiting, id)
	a.mu.Unlock()

	if waiter == nil {
		return
	}
	addr, err := parseBindingResponse(data, id)
	if err != nil {
		return
	}
	select {
	case waiter <- addr:
	default:
	}
}

func (a *Agent) discover() error {
	server, err := resolveUDP(a.cfg.STUNServer)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		id, err := newTransactionID()
		if err != nil {
			return err
		}

		waiter := make(chan netip.AddrPort, 1)
		a.mu.Lock()
		a.waiting[id] = waiter
		a.mu.Unlock()

		if _, err := a.conn.WriteToUDP(buildBindingRequest(id), server); err != nil {
			a.forgetWaiter(id)
			return err
		}

		select {
		case addr := <-waiter:
			a.mu.Lock()
			a.public = addr
			a.mu.Unlock()
			return nil
		case <-time.After(stunAttemptInterval):
			a.forgetWaiter(id)
			lastErr = fmt.Errorf("p2p: no stun response from %s", a.cfg.STUNServer)
		case <-a.closeCh:
			a.forgetWaiter(id)
			return errors.New("p2p: agent closed")
		}
	}
	return lastErr
}

func (a *Agent) forgetWaiter(id transactionID) {
	a.mu.Lock()
	delete(a.waiting, id)
	a.mu.Unlock()
}

func (a *Agent) signalLoop() {
	defer a.wg.Done()
	ticker := time.NewTicker(signalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.closeCh:
			return
		case <-ticker.C:
			a.pollSignals()
		}
	}
}

func (a *Agent) pollSignals() {
	if a.cfg.Signaler == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), signalTimeout)
	defer cancel()

	signals, err := a.cfg.Signaler.TakeSignals(ctx)
	if err != nil {
		return
	}
	for _, signal := range signals {
		a.applySignal(signal)
	}
}

func (a *Agent) applySignal(signal Signal) {
	if signal.From == "" || signal.From == a.cfg.PublicKey {
		return
	}
	tok, err := parseToken(signal.Token)
	if err != nil {
		return
	}

	a.mu.Lock()
	s, exists := a.sessions[signal.From]
	if !exists {
		peer := Peer{PublicKey: signal.From, Name: signal.Name, MeshIP: signal.MeshIP}
		s = newSession(peer, signal.Kind == SignalAnswer, tok)
		a.sessions[signal.From] = s
	}
	a.mu.Unlock()

	s.addCandidates(parseCandidates(signal.Candidates))

	if signal.Kind != SignalOffer || a.cfg.Signaler == nil {
		return
	}

	answer := Signal{
		Kind:       SignalAnswer,
		From:       a.cfg.PublicKey,
		Name:       a.cfg.Name,
		MeshIP:     a.cfg.MeshIP,
		Token:      tok.String(),
		Candidates: a.candidateStrings(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), signalTimeout)
	defer cancel()
	_ = a.cfg.Signaler.PostSignal(ctx, signal.From, answer)
}

func (a *Agent) punchLoop() {
	defer a.wg.Done()
	ticker := time.NewTicker(punchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.closeCh:
			return
		case <-ticker.C:
			a.tick()
		}
	}
}

func (a *Agent) tick() {
	for _, s := range a.snapshot() {
		if payload, targets, ok := s.outgoing(); ok {
			a.sendTo(payload, targets)
			continue
		}
		if payload, targets := s.keepalive(); payload != nil {
			a.sendTo(payload, targets)
		}
		s.expire()
	}
}

func (a *Agent) sendTo(payload []byte, targets []netip.AddrPort) {
	for _, target := range targets {
		if _, err := a.conn.WriteToUDP(payload, net.UDPAddrFromAddrPort(target)); err != nil {
			continue
		}
	}
}
