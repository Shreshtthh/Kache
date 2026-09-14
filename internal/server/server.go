package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"github.com/kache-store/kache/internal/command"
	"github.com/kache-store/kache/internal/protocol"
)

// Server is the TCP server that accepts client connections and dispatches commands.
type Server struct {
	addr     string
	executor *command.Executor
	listener net.Listener
	wg       sync.WaitGroup
	active   int64 // atomic — number of active connections
	ready chan struct{}
}

// NewServer creates a new TCP server.
func NewServer(addr string, executor *command.Executor) *Server {
	return &Server{
		addr:     addr,
		executor: executor,
		ready: make(chan struct{}),
	}
}

// ListenAndServe starts the TCP server. It blocks until the context is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}
	s.listener = ln
	close(s.ready)
	log.Printf("kache server listening on %s", s.addr)

	// Goroutine to close the listener when context is cancelled.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Check if we're shutting down.
			select {
			case <-ctx.Done():
				s.wg.Wait()
				log.Println("server shut down gracefully")
				return nil
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}

		s.wg.Add(1)
		atomic.AddInt64(&s.active, 1)
		go s.handleConnection(ctx, conn)
	}
}

// handleConnection processes a single client connection.
// One goroutine per connection — reads frames, dispatches commands, writes responses.
func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer func() {
		conn.Close()
		atomic.AddInt64(&s.active, -1)
		s.wg.Done()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Read a frame from the connection.
		payload, err := protocol.DecodeFrame(conn)
		if err != nil {
			// Connection closed or protocol error — close silently.
			return
		}

		// Parse the command.
		cmd, err := protocol.ParseCommand(payload)
		if err != nil {
			resp := protocol.NewErrorResponse(err.Error())
			s.writeResponse(conn, resp)
			continue
		}

		// Execute the command.
		resp := s.executor.Execute(cmd)

		// Write the response.
		if err := s.writeResponse(conn, resp); err != nil {
			return
		}
	}
}

// writeResponse serializes and sends a response to the client.
func (s *Server) writeResponse(conn net.Conn, resp *protocol.Response) error {
	respPayload := protocol.SerializeResponse(resp)
	frame, err := protocol.EncodeFrame(respPayload)
	if err != nil {
		return err
	}
	_, err = conn.Write(frame)
	return err
}

// ActiveConnections returns the number of currently active connections.
func (s *Server) ActiveConnections() int64 {
	return atomic.LoadInt64(&s.active)
}

// Addr returns the listener's address (useful when binding to port 0 for tests).
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.addr
}

func (s *Server) Ready() <-chan struct{}{
	return s.ready
}
