package server

import (
	controller "antenna-rotator-server/rotator-controller"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	HttpServer http.Server
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func CreateServer() *Server {
	mux := http.NewServeMux()

	rotator := initRotator()

	mux.HandleFunc("/set-heading", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		heading := r.URL.Query().Get("heading")
		if heading == "" {
			writeError(w, http.StatusBadRequest, "Missing heading parameter")
			return
		}
		if rotator == nil {
			writeError(w, http.StatusInternalServerError, "Rotator controller not initialized")
			return
		}
		if err := rotator.SetHeading(heading); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		deg, _ := strconv.Atoi(strings.TrimSpace(heading))
		writeJSON(w, http.StatusOK, headingBody{Heading: deg, Status: "set"})
	})

	mux.HandleFunc("/get-heading", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		if rotator == nil {
			writeError(w, http.StatusInternalServerError, "Rotator controller not initialized")
			return
		}
		heading, err := rotator.GetHeading()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		deg, err := strconv.Atoi(strings.TrimSpace(heading))
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("invalid heading from controller: %q", heading))
			return
		}
		writeJSON(w, http.StatusOK, headingBody{Heading: deg})
	})

	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		if rotator == nil {
			writeError(w, http.StatusInternalServerError, "Rotator controller not initialized")
			return
		}
		if err := rotator.Stop(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, stopBody{Status: "stopped"})
	})

	mux.HandleFunc("/list-ports", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
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

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	registerDocsRoutes(mux)

	return &Server{
		HttpServer: http.Server{
			Addr:    ":8080",
			Handler: mux,
		},
	}
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
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(swaggerHTML)
	}
	mux.HandleFunc("/swagger", serveSwagger)
	mux.HandleFunc("/swagger/", serveSwagger)

	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPI)
	})

	// Optional: serve any future static asset placed under static/ (e.g. a
	// vendored swagger-ui-dist bundle for offline use).
	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/swagger/", http.StatusFound)
	})
}

// initRotator opens the port named by ROTATOR_PORT, or auto-detects the first
// available port if the env var is not set. If no port is available it falls
// back to a simulation-mode controller so the API stays usable for testing.
func initRotator() *controller.RotatorController {
	portName := os.Getenv("ROTATOR_PORT")
	var (
		r   *controller.RotatorController
		err error
	)
	if portName != "" {
		r, err = controller.NewRotatorControllerWithPort(portName)
	} else {
		r, err = controller.NewRotatorController()
	}
	if err != nil {
		fmt.Printf("Rotator controller unavailable (%v) — running in simulation mode\n", err)
		return controller.NewSimulationController()
	}
	if portName == "" {
		fmt.Println("Rotator controller connected on auto-detected port")
	} else {
		fmt.Printf("Rotator controller connected on %s\n", portName)
	}
	return r
}

func (s *Server) StartServer() {
	fmt.Println("Starting server on :8080 ...")
	fmt.Println("Swagger UI: http://localhost:8080/swagger/")
	if err := s.HttpServer.ListenAndServe(); err != nil {
		fmt.Println("Server error:", err)
	}
}
