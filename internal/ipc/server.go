package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

type Handler func(params json.RawMessage) (any, error)

type Server struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func NewServer() *Server {
	return &Server{handlers: make(map[string]Handler)}
}

func (s *Server) Handle(method string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = handler
}

func (s *Server) Serve(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		var req Request
		if err := readMessage(reader, &req); err != nil {
			return
		}
		if err := writeMessage(conn, s.dispatch(&req)); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(req *Request) *Response {
	s.mu.RLock()
	handler, ok := s.handlers[req.Method]
	s.mu.RUnlock()
	if !ok {
		return &Response{ID: req.ID, Error: fmt.Sprintf("unknown method %q", req.Method)}
	}

	result, err := handler(req.Params)
	if err != nil {
		return &Response{ID: req.ID, Error: err.Error()}
	}
	if result == nil {
		return &Response{ID: req.ID}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return &Response{ID: req.ID, Error: err.Error()}
	}
	return &Response{ID: req.ID, Result: data}
}
