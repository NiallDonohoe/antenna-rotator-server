package main

import (
	controller "antenna-rotator-server/rotator-controller"
	server "antenna-rotator-server/rotator-server"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

const shutdownTimeout = 10 * time.Second

func main() {
	setupLogging()
	slog.Info("starting antenna-rotator-server", "version", version)

	rotator, err := buildRotator()
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer rotator.Close()

	srv := server.New(server.Config{
		Addr:    listenAddr(),
		Rotator: rotator,
		Version: version,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr(), "swagger", "http://localhost"+srv.Addr()+"/swagger/")
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
//
// A hardware server must never silently pretend to work, so with neither
// SIMULATION nor ROTATOR_PORT set startup fails with a hint listing the
// ports it can see.
func buildRotator() (*controller.RotatorController, error) {
	offset := 0
	if o := os.Getenv("ROTATOR_HEADING_OFFSET"); o != "" {
		v, err := strconv.Atoi(strings.TrimSpace(o))
		if err != nil {
			return nil, fmt.Errorf("invalid ROTATOR_HEADING_OFFSET %q: must be an integer", o)
		}
		offset = v
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

	rotator, err := controller.NewRotatorControllerWithPort(portName, opts)
	if err != nil {
		return nil, err
	}
	rotator.SetOffset(offset)
	slog.Info("rotator controller connected", "port", portName, "protocol", rotator.Protocol())
	if offset != 0 {
		slog.Info("heading offset applied", "offset", offset)
	}
	return rotator, nil
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

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
