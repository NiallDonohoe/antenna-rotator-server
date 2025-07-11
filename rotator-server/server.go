package server

import (
	controller "antenna-rotator-server/rotator-controller"
	"fmt"
	"net/http"
)

type Server struct {
	HttpServer http.Server
}

func CreateServer() *Server {
	mux := http.NewServeMux()
	// Initialize the rotator controller (replace with your device's VID and PID)
	rotator, err := controller.NewRotatorController(0x1234, 0x5678)
	if err != nil {
		fmt.Println("Error initializing rotator controller:", err)
	}
	mux.HandleFunc("/set-heading", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		heading := r.URL.Query().Get("heading")
		if heading == "" {
			http.Error(w, "Missing heading parameter", http.StatusBadRequest)
			return
		}
		if rotator == nil {
			http.Error(w, "Rotator controller not initialized", http.StatusInternalServerError)
			return
		}
		err := rotator.SetHeading(heading)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "Heading set to %s", heading)
	})
	return &Server{
		HttpServer: http.Server{
			Addr:    ":8080",
			Handler: mux,
		},
	}
}

func (s *Server) StartServer() {
	fmt.Println("Starting Server on :8080 ...")
	if err := s.HttpServer.ListenAndServe(); err != nil {
		fmt.Println("Server error:", err)
	}
}
