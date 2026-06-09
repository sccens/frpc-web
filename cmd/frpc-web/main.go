package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sccens/frpc-web/internal/app"
	"github.com/sccens/frpc-web/internal/frpc"
	"github.com/sccens/frpc-web/internal/server"
	"github.com/sccens/frpc-web/internal/storage"
	webui "github.com/sccens/frpc-web/web"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := getenv("FRPC_WEB_ADDR", "127.0.0.1:8080")
	dataDir := getenv("FRPC_WEB_DATA_DIR", "frpc-web-data")
	trustProxyHeaders := truthy(os.Getenv("FRPC_WEB_TRUSTED_PROXY"))

	store, err := storage.Open(ctx, dataDir)
	if err != nil {
		logger.Error("open storage failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	runtime := frpc.New(dataDir)
	svc := app.NewService(app.Options{
		Store:   store,
		Runtime: runtime,
		Addr:    addr,
	})
	if err := svc.Restore(ctx); err != nil {
		logger.Warn("restore runtime state failed", "error", err)
	}

	handler := server.New(server.Options{
		Service:           svc,
		Logger:            logger,
		WebDir:            os.Getenv("FRPC_WEB_WEB_DIR"),
		WebFS:             webui.FileSystem(),
		TrustProxyHeaders: trustProxyHeaders,
	})

	if isPublicBind(addr) {
		logger.Warn("frpc-web is listening on a public address; login auth is enabled, but HTTPS and reverse proxy access control are still recommended", "addr", addr)
	}
	if trustProxyHeaders {
		logger.Warn("trusted proxy headers enabled; X-Forwarded-For and X-Real-IP will be used for audit and rate limiting")
	}

	httpServer := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("starting frpc-web", "addr", addr, "data_dir", dataDir)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func isPublicBind(addr string) bool {
	return strings.HasPrefix(addr, "0.0.0.0:") || strings.HasPrefix(addr, "[::]:") || strings.HasPrefix(addr, ":")
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
