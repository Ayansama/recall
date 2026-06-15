package mcp

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/mark3labs/mcp-go/server"

	"recall/internal/storage"
)

// Server exposes Recall data over MCP via a local Unix domain socket.
type Server struct {
	db       *storage.DB
	mcp      *server.MCPServer
	stdio    *server.StdioServer
	path     string
	listener net.Listener

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// New creates an MCP server bound to the default socket path.
func New(db *storage.DB) (*Server, error) {
	path, err := DefaultSocketPath()
	if err != nil {
		return nil, fmt.Errorf("resolve socket path: %w", err)
	}
	return NewAt(db, path)
}

// NewAt creates an MCP server bound to a specific socket path.
func NewAt(db *storage.DB, path string) (*Server, error) {
	mcpServer := server.NewMCPServer(
		"recall",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)
	registerTools(mcpServer, db)

	return &Server{
		db:    db,
		mcp:   mcpServer,
		stdio: server.NewStdioServer(mcpServer),
		path:  path,
	}, nil
}

// Path returns the filesystem path of the Unix domain socket.
func (s *Server) Path() string {
	return s.path
}

// Start listens on the Unix domain socket and serves MCP clients in the background.
func (s *Server) Start(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("listen on unix socket: %w", err)
	}
	s.listener = listener

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(runCtx)
	}()

	return nil
}

// Stop closes the listener and waits for active connections to finish.
func (s *Server) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.wg.Wait()
	_ = os.Remove(s.path)
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				if !isClosedListener(err) {
					log.Printf("recall mcp: accept: %v", err)
				}
				return
			}
		}

		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer c.Close()
			if err := s.serveConn(ctx, c); err != nil && err != io.EOF {
				log.Printf("recall mcp: connection: %v", err)
			}
		}(conn)
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) error {
	return s.stdio.Listen(ctx, conn, conn)
}

func isClosedListener(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "use of closed network connection" ||
		err.Error() == "accept unix "+": use of closed network connection"
}
