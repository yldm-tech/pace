// Command live runs the Go replacement for the Node collaborative editor service.
//
// It serves the websocket every open page is connected to and the few HTTP routes beside it. Like the service it replaces, it holds a document in memory for as long as somebody has it open and writes it back to the API through the editing user's own session, so the API's permissions are what decide who may read and write a page.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yldm-tech/pace/apps/api-go/internal/live"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	config, err := live.LoadConfig()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	server := live.NewServer(config, logger)
	if err := server.Run(ctx); err != nil {
		logger.Error("live server stopped", "error", err)
		os.Exit(1)
	}
	logger.Info("live server shut down")
}
