package main

import (
	controller "antenna-rotator-server/rotator-controller"
	server "antenna-rotator-server/rotator-server"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var version = buildVersion()

const shutdownTimeout = 10 * time.Second

func main() {
	setupLogging()
	slog.Info("starting antenna-rotator-server", "version", version)

	offsetFile := os.Getenv("ROTATOR_OFFSET_FILE")
	rotator, err := buildRotator(offsetFile)
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer rotator.Close()

	cfg := server.Config{
		Addr:    listenAddr(),
		Rotator: rotator,
		Version: version,
	}
	if offsetFile != "" {
		cfg.SaveOffset = func(offset int) error { return saveOffset(offsetFile, offset) }
	} else {
		slog.Warn("ROTATOR_OFFSET_FILE is not set: an offset set via the API will be lost on restart")
	}
	srv := server.New(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr(), "swagger", swaggerURL(srv.Addr()))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining requests", "timeout", shutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
	}
	slog.Info("server stopped")
}

// buildRotator picks the controller from the environment:
//
//	SIMULATION=true       → in-memory simulated rotator (explicit opt-in only)
//	ROTATOR_PORT=...      → open that serial port; failure aborts startup
//	ROTATOR_PROTOCOL=...  → "prosistel" (default) or "yaesu" (GS-232A/B)
//	ROTATOR_BAUD=...      → serial baud rate (default 9600)
//	ROTATOR_HEADING_OFFSET=... → calibration offset in degrees, added to
//	                             raw rotator readings (default 0)
//	ROTATOR_OFFSET_FILE=...    → where POST /offset saves the offset; a saved
//	                             value takes precedence over the env var
//
// A hardware server must never silently pretend to work, so with neither
// SIMULATION nor ROTATOR_PORT set startup fails with a hint listing the
// ports it can see. A port that is set but can't be opened yet is only a
// warning: the controller retries on every request, so a USB adapter that
// enumerates late after a power cut is picked up without a restart.
func buildRotator(offsetFile string) (*controller.RotatorController, error) {
	offset, err := initialOffset(offsetFile)
	if err != nil {
		return nil, err
	}

	if isTruthy(os.Getenv("SIMULATION")) {
		slog.Warn("running in SIMULATION mode — no hardware will be driven")
		rotator := controller.NewSimulationController()
		rotator.SetOffset(offset)
		return rotator, nil
	}

	portName := os.Getenv("ROTATOR_PORT")
	if portName == "" {
		hint := "none detected"
		if ports, err := controller.ListAvailablePorts(); err == nil && len(ports) > 0 {
			hint = strings.Join(ports, ", ")
		}
		return nil, fmt.Errorf(
			"ROTATOR_PORT is not set (available ports: %s); set ROTATOR_PORT to a serial port, or SIMULATION=true to run without hardware", hint)
	}

	opts := controller.Options{Protocol: os.Getenv("ROTATOR_PROTOCOL")}
	if b := os.Getenv("ROTATOR_BAUD"); b != "" {
		baud, err := strconv.Atoi(b)
		if err != nil || baud <= 0 {
			return nil, fmt.Errorf("invalid ROTATOR_BAUD %q: must be a positive integer", b)
		}
		opts.BaudRate = baud
	}

	rotator, err := controller.NewRotatorController(portName, opts)
	if err != nil {
		return nil, err
	}
	rotator.SetOffset(offset)
	if err := rotator.Connect(); err != nil {
		slog.Warn("serial port not available yet, will retry on each request", "port", portName, "error", err)
	} else {
		slog.Info("rotator controller connected", "port", portName, "protocol", rotator.Protocol())
	}
	if offset != 0 {
		slog.Info("heading offset applied", "offset", offset)
	}
	return rotator, nil
}

// initialOffset picks the calibration offset to start with: the one a
// previous POST /offset saved to offsetFile if there is one, otherwise
// ROTATOR_HEADING_OFFSET, otherwise 0.
func initialOffset(offsetFile string) (int, error) {
	if offsetFile != "" {
		offset, ok, err := loadOffset(offsetFile)
		if err != nil {
			return 0, err
		}
		if ok {
			slog.Info("loaded saved heading offset", "file", offsetFile, "offset", offset)
			return offset, nil
		}
	}
	if o := os.Getenv("ROTATOR_HEADING_OFFSET"); o != "" {
		v, err := strconv.Atoi(strings.TrimSpace(o))
		if err != nil {
			return 0, fmt.Errorf("invalid ROTATOR_HEADING_OFFSET %q: must be an integer", o)
		}
		return v, nil
	}
	return 0, nil
}

// swaggerURL turns a listen address into a clickable Swagger UI link for the
// startup log. A wildcard bind like ":8080" or "0.0.0.0:8080" becomes
// localhost; a specific IP is kept.
func swaggerURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + "/swagger/"
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/swagger/"
}

func listenAddr() string {
	if addr := os.Getenv("LISTEN_ADDR"); addr != "" {
		return addr
	}
	return ":8080"
}

func setupLogging() {
	level := slog.LevelInfo
	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		if err := level.UnmarshalText([]byte(lvl)); err != nil {
			fmt.Fprintf(os.Stderr, "invalid LOG_LEVEL %q, using info\n", lvl)
		}
	}
	var handler slog.Handler
	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "json") {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(handler))
}

// buildVersion reports the version Go records for the main module at build
// time: the git tag (e.g. v1.0.0) when building a tagged commit, otherwise a
// pseudo-version naming the commit, with +dirty for uncommitted changes.
// `go run` and builds outside a git checkout record nothing, giving "dev".
func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
