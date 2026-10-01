// Loopback HTTP lifecycle for the Web Board.
//
// Derived from rhizome-mcp (https://github.com/Odrin/rhizome-mcp)
// internal/runtime/http_lifecycle.go, via boboty/agent-board-rhizome-poc,
// licensed under Apache-2.0. See NOTICE. Modified: dropped MCP request
// logging; the Host check also accepts "localhost" on the bound port; the
// null-Origin allowance is always on because every write route carries its
// own synchronizer token.

package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	errInvalidRequestHost  = errors.New("invalid request host")
	errMisdirectedRequest  = errors.New("misdirected request")
	errOriginMismatch      = errors.New("origin mismatch")
	errRequestBodyTooLarge = errors.New("request body too large")
)

const (
	defaultReadHeaderTimeout = 10 * time.Second
	defaultReadTimeout       = 30 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultMaxHeaderBytes    = 8 << 10
	defaultMaxBodyBytes      = 1 << 20
	defaultShutdownTimeout   = 5 * time.Second
)

// ServerOptions configures the loopback-only HTTP lifecycle.
type ServerOptions struct {
	// Address is a literal loopback address such as 127.0.0.1:7420; port 0
	// picks a free port.
	Address         string
	Handler         http.Handler
	Logger          *slog.Logger
	ShutdownTimeout time.Duration
	// OnListen is called with the bound listener before serving starts.
	OnListen func(net.Listener)
	Listen   func(network, address string) (net.Listener, error)
}

// Serve listens on a loopback address and serves until ctx is canceled. Every
// request passes the Host/Origin checks, a body size limit, and panic
// recovery before it reaches the handler.
func Serve(ctx context.Context, options ServerOptions) error {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if options.ShutdownTimeout <= 0 {
		options.ShutdownTimeout = defaultShutdownTimeout
	}
	address, err := ValidateLoopbackAddress(options.Address)
	if err != nil {
		return err
	}
	listen := options.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", address)
	if err != nil {
		return err
	}
	if options.OnListen != nil {
		options.OnListen(listener)
	}

	// Request contexts derive from baseCtx, which is canceled once Shutdown
	// returns so handlers still in flight unwind promptly.
	baseCtx, cancelBaseCtx := context.WithCancel(context.Background())
	defer cancelBaseCtx()

	server := &http.Server{
		Handler:           harden(options.Handler, listener.Addr().String(), options.Logger, defaultMaxBodyBytes),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		MaxHeaderBytes:    defaultMaxHeaderBytes,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}
	serveErrs := make(chan error, 1)
	go func() { serveErrs <- server.Serve(listener) }()

	select {
	case err := <-serveErrs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), options.ShutdownTimeout)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		cancelBaseCtx()
		if shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
			return shutdownErr
		}
		if err := <-serveErrs; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// ValidateLoopbackAddress parses a literal loopback bind address. IPv4 127/8
// and IPv6 ::1 are accepted; hostnames and other addresses are rejected.
func ValidateLoopbackAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return "", fmt.Errorf("invalid http address %q: %w", address, err)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 0 || portNum > 65535 {
		return "", fmt.Errorf("invalid http address %q: invalid port %q", address, port)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("invalid http address %q: only literal loopback addresses are allowed", address)
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 127 {
			return net.JoinHostPort(ip4.String(), port), nil
		}
		return "", fmt.Errorf("invalid http address %q: only 127/8 is allowed", address)
	}
	if ip.Equal(net.IPv6loopback) {
		return net.JoinHostPort("::1", port), nil
	}
	return "", fmt.Errorf("invalid http address %q: only 127/8 and ::1 are allowed", address)
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	if r.wroteHeader {
		return
	}
	r.statusCode = statusCode
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(data)
}

type limitedBody struct {
	io.ReadCloser
	limit    int64
	read     int64
	exceeded bool
}

func (r *limitedBody) Read(data []byte) (int, error) {
	if r.exceeded {
		return 0, errRequestBodyTooLarge
	}
	n, err := r.ReadCloser.Read(data)
	r.read += int64(n)
	if r.read > r.limit {
		r.exceeded = true
		if err == nil {
			err = errRequestBodyTooLarge
		}
	}
	return n, err
}

// harden validates Host and Origin against the bound authority, limits the
// request body, recovers panics as 500 without leaking them, and logs each
// request.
func harden(handler http.Handler, authority string, logger *slog.Logger, maxBodyBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		var body *limitedBody
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("http handler panic", "error", recovered)
				if !recorder.wroteHeader {
					recorder.WriteHeader(http.StatusInternalServerError)
				}
			}
			if body != nil && body.exceeded && !recorder.wroteHeader {
				recorder.WriteHeader(http.StatusRequestEntityTooLarge)
			}
			logger.Info("http request", "method", request.Method, "path", request.URL.Path,
				"status", recorder.statusCode, "duration", time.Since(startedAt))
		}()
		if err := validateRequest(request, authority); err != nil {
			switch {
			case errors.Is(err, errMisdirectedRequest):
				http.Error(recorder, http.StatusText(http.StatusMisdirectedRequest), http.StatusMisdirectedRequest)
			case errors.Is(err, errOriginMismatch):
				http.Error(recorder, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			default:
				http.Error(recorder, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			}
			return
		}
		if request.ContentLength > maxBodyBytes {
			http.Error(recorder, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
			return
		}
		if request.Body != nil {
			body = &limitedBody{ReadCloser: request.Body, limit: maxBodyBytes}
			request.Body = body
		}
		handler.ServeHTTP(recorder, request)
	})
}

// validateRequest rejects DNS-rebinding and cross-origin requests: the Host
// must name the bound loopback port (as the bound IP or "localhost"), and an
// Origin, when present, must be that same host. A literal "null" Origin is
// accepted only with same-origin fetch metadata.
func validateRequest(request *http.Request, authority string) error {
	bound, err := parseAuthority(authority)
	if err != nil {
		return errInvalidRequestHost
	}
	requestHost := strings.TrimSpace(request.Host)
	got, err := parseAuthority(requestHost)
	if err != nil {
		return errInvalidRequestHost
	}
	if got.port != bound.port || (got.host != bound.host && got.host != "localhost") {
		return errMisdirectedRequest
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return nil
	}
	if origin == "null" {
		site := strings.ToLower(strings.TrimSpace(request.Header.Get("Sec-Fetch-Site")))
		if site == "same-origin" || site == "none" {
			return nil
		}
		return errOriginMismatch
	}
	if origin != "http://"+requestHost {
		return errOriginMismatch
	}
	return nil
}

type parsedAuthority struct{ host, port string }

func parseAuthority(value string) (parsedAuthority, error) {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return parsedAuthority{}, err
	}
	if host == "" || port == "" {
		return parsedAuthority{}, errInvalidRequestHost
	}
	if ip := net.ParseIP(host); ip != nil {
		return parsedAuthority{host: ip.String(), port: port}, nil
	}
	return parsedAuthority{host: strings.ToLower(host), port: port}, nil
}
