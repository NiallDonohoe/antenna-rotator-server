package server

import (
	controller "antenna-rotator-server/rotator-controller"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed static
var staticFS embed.FS

// Rotator is the surface the HTTP layer needs from a rotator controller.
// *controller.RotatorController implements it; tests substitute fakes.
type Rotator interface {
	SetHeading(deg int) error
	GetHeading() (int, error)
	Stop() error
	Mode() string
	Protocol() string
	Connected() bool
}

type Config struct {
	Addr    string // listen address, e.g. ":8080"
	Rotator Rotator
	Version string // reported by /healthz
}

type Server struct {
	httpServer *http.Server
}

type errorBody struct {
	Error string `json:"error"`
}

type headingBody struct {
	Heading int    `json:"heading"`
	Status  string `json:"status,omitempty"`
}

type stopBody struct {
	Status string `json:"status"`
}

type portsBody struct {
	Ports []string `json:"ports"`
}

type healthBody struct {
	Status    string `json:"status"`
	Mode      string `json:"mode"`
	Protocol  string `json:"protocol"`
	Connected bool   `json:"connected"`
	Version   string `json:"version"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// writeRotatorError maps controller errors to HTTP statuses: validation
// problems are the client's fault (400), anything else is a serial I/O
// failure upstream of this server (502).
func writeRotatorError(w http.ResponseWriter, err error) {
	if errors.Is(err, controller.ErrInvalidHeading) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func New(cfg Config) *Server {
	if cfg.Rotator == nil {
		panic("server.New: Config.Rotator must not be nil")
	}
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}

	mux := http.NewServeMux()
	registerAPIRoutes(mux, cfg)
	registerDocsRoutes(mux)

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.Addr,
			Handler:           requestLogger(mux),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}
}

// registerAPIRoutes wires every API endpoint under both its legacy
// unprefixed path and the canonical /api/v1 prefix.
func registerAPIRoutes(mux *http.ServeMux, cfg Config) {
	rotator := cfg.Rotator

	handle := func(pattern string, h http.HandlerFunc) {
		method, path, _ := strings.Cut(pattern, " ")
		mux.HandleFunc(method+" "+path, h)
		mux.HandleFunc(method+" /api/v1"+path, h)
	}

	handle("POST /set-heading", func(w http.ResponseWriter, r *http.Request) {
		deg, err := parseHeadingRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := rotator.SetHeading(deg); err != nil {
			writeRotatorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, headingBody{Heading: deg, Status: "set"})
	})

	handle("GET /get-heading", func(w http.ResponseWriter, r *http.Request) {
		heading, err := rotator.GetHeading()
		if err != nil {
			writeRotatorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, headingBody{Heading: heading})
	})

	handle("POST /stop", func(w http.ResponseWriter, r *http.Request) {
		if err := rotator.Stop(); err != nil {
			writeRotatorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, stopBody{Status: "stopped"})
	})

	handle("GET /list-ports", func(w http.ResponseWriter, r *http.Request) {
		ports, err := controller.ListAvailablePorts()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ports == nil {
			ports = []string{}
		}
		writeJSON(w, http.StatusOK, portsBody{Ports: ports})
	})

	handle("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, healthBody{
			Status:    "ok",
			Mode:      rotator.Mode(),
			Protocol:  rotator.Protocol(),
			Connected: rotator.Connected(),
			Version:   cfg.Version,
		})
	})
}

// parseHeadingRequest accepts the heading either as a `?heading=` query
// parameter or as a JSON body `{"heading": N}`.
func parseHeadingRequest(r *http.Request) (int, error) {
	if q := r.URL.Query().Get("heading"); q != "" {
		deg, err := strconv.Atoi(strings.TrimSpace(q))
		if err != nil {
			return 0, fmt.Errorf("invalid heading %q: must be an integer", q)
		}
		return deg, nil
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			Heading *int `json:"heading"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1024))
		if err := dec.Decode(&body); err != nil {
			return 0, fmt.Errorf("invalid JSON body: %v", err)
		}
		if body.Heading == nil {
			return 0, fmt.Errorf("JSON body missing \"heading\" field")
		}
		return *body.Heading, nil
	}
	return 0, fmt.Errorf("missing heading: pass ?heading=N or a JSON body {\"heading\": N}")
}

// registerDocsRoutes wires up the Swagger UI page, the OpenAPI spec, and a
// root redirect so visitors landing on `/` end up at the docs.
func registerDocsRoutes(mux *http.ServeMux) {
	swaggerHTML, err := staticFS.ReadFile("static/swagger.html")
	if err != nil {
		panic(fmt.Sprintf("embedded swagger.html missing: %v", err))
	}
	openAPI, err := staticFS.ReadFile("static/openapi.yaml")
	if err != nil {
		panic(fmt.Sprintf("embedded openapi.yaml missing: %v", err))
	}

	serveSwagger := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(swaggerHTML)
	}
	mux.HandleFunc("GET /swagger", serveSwagger)
	mux.HandleFunc("GET /swagger/", serveSwagger)

	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPI)
	})

	// Optional: serve any future static asset placed under static/ (e.g. a
	// vendored swagger-ui-dist bundle for offline use).
	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	}

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/", http.StatusFound)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// requestLogger logs one line per request via slog. Health probes are logged
// at Debug so they don't drown out real traffic.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		if strings.HasSuffix(r.URL.Path, "/healthz") {
			level = slog.LevelDebug
		}
		slog.LogAttrs(r.Context(), level, "request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
			slog.String("remote", r.RemoteAddr),
		)
	})
}

// Handler exposes the root handler for tests.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Addr reports the configured listen address.
func (s *Server) Addr() string {
	return s.httpServer.Addr
}

// ListenAndServe blocks serving HTTP until Shutdown is called or the
// listener fails. It returns http.ErrServerClosed after a clean shutdown.
func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully drains in-flight requests.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
