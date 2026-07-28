package http

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog/log"

	"example.invalid/mcp-template-module-placeholder/pkg/core/config"
	mcpserver "example.invalid/mcp-template-module-placeholder/pkg/server/mcp"
)

const (
	HealthEndpoint     = "/healthz"
	MCPEndpoint        = "/mcp"
	SSEEndpoint        = "/sse"
	SSEMessageEndpoint = "/message"
)

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 30 * time.Second
	// WriteTimeout must be 0 for SSE / long-lived MCP streams; a positive
	// deadline is counted from request headers and cuts open connections.
	defaultWriteTimeout = 0
	defaultIdleTimeout  = 120 * time.Second
)

// Serve starts HTTP transports for MCP and blocks until shutdown.
func Serve(ctx context.Context, mcpServer *mcpserver.Server, staticConfig *config.StaticConfig) error {
	if staticConfig == nil {
		return errors.New("static config is required")
	}

	listener, err := net.Listen("tcp", staticConfig.GetListenAddress())
	if err != nil {
		return err
	}
	err = ServeListener(ctx, mcpServer, staticConfig, listener)
	// Shutdown already closes the listener via http.Server; ignore double-close.
	_ = listener.Close()
	return err
}

// ServeListener starts HTTP transports for MCP on the provided listener and blocks until shutdown.
func ServeListener(ctx context.Context, mcpServer *mcpserver.Server, staticConfig *config.StaticConfig, listener net.Listener) error {
	if mcpServer == nil {
		return errors.New("MCP server is required")
	}
	if staticConfig == nil {
		return errors.New("static config is required")
	}
	if listener == nil {
		return errors.New("listener is required")
	}

	httpServer := &http.Server{
		Addr:              listener.Addr().String(),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}
	handler := NewHandler(mcpServer, httpServer, staticConfig.SSEBaseURL)
	httpServer.Handler = RequestMiddleware(handler)
	return runServer(ctx, httpServer, handler, listener)
}

func runServer(ctx context.Context, httpServer *http.Server, handler *Handler, listener net.Listener) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		log.Info().
			Str("addr", httpServer.Addr).
			Str("mcp", MCPEndpoint).
			Str("sse", SSEEndpoint).
			Str("message", SSEMessageEndpoint).
			Msg("starting HTTP MCP server")
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		serveErr = err
	}

	select {
	case err := <-serverErr:
		if serveErr == nil {
			serveErr = err
		}
	default:
	}

	if err := handler.Shutdown(9 * time.Second); err != nil {
		if serveErr != nil {
			return errors.Join(serveErr, err)
		}
		return err
	}
	<-serverDone
	return serveErr
}

// Handler owns mounted MCP HTTP transports so they can be shut down cleanly.
type Handler struct {
	mux                  *http.ServeMux
	shutdownCtx          context.Context
	shutdownCancel       context.CancelFunc
	sseServer            *mcpgo.SSEServer
	streamableHTTPServer *mcpgo.StreamableHTTPServer
	httpServer           *http.Server
}

// NewHandler wires HTTP routes to MCP transport handlers.
func NewHandler(mcpServer *mcpserver.Server, httpServer *http.Server, sseBaseURL string) *Handler {
	if mcpServer == nil {
		panic("mcp server is required")
	}
	if httpServer == nil {
		panic("http server is required")
	}

	mux := http.NewServeMux()
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

	sseServer := mcpServer.ServeSSE(sseBaseURL, httpServer)
	streamableHTTPServer := mcpServer.ServeStreamableHTTP(httpServer)

	handler := &Handler{
		mux:                  mux,
		shutdownCtx:          shutdownCtx,
		shutdownCancel:       shutdownCancel,
		sseServer:            sseServer,
		streamableHTTPServer: streamableHTTPServer,
		httpServer:           httpServer,
	}

	mux.Handle(SSEEndpoint, sseServer.SSEHandler())
	mux.Handle(SSEMessageEndpoint, sseServer.MessageHandler())
	mux.Handle(MCPEndpoint, handler.withShutdownContext(streamableHTTPServer))
	mux.HandleFunc(HealthEndpoint, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		status, body := http.StatusServiceUnavailable, "unhealthy"
		if mcpServer.IsHealthy() {
			status, body = http.StatusOK, "healthy"
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})

	return handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) withShutdownContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			select {
			case <-h.shutdownCtx.Done():
				cancel()
			case <-ctx.Done():
			}
		}()

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Shutdown closes active MCP transport sessions and the underlying HTTP server.
// SSE/streamable wrappers share this http.Server; do not call their Shutdown
// helpers because they would shut down the same server more than once.
func (h *Handler) Shutdown(timeout time.Duration) error {
	h.shutdownCancel()

	if h.sseServer != nil {
		h.sseServer.CloseSessions()
	}

	if h.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return h.httpServer.Shutdown(ctx)
}
