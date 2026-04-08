package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/xiaoxin-zk/cpa-account-biopsy-system/internal/accounthealth"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := accounthealth.NewAppFromEnv()
	if err != nil {
		log.Fatalf("account-health config error: %v", err)
	}

	go app.Run(ctx)

	server := &http.Server{
		Addr:    app.ListenAddr(),
		Handler: app.Handler(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), app.ShutdownTimeout())
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("account-health listening on %s", app.ListenAddr())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("account-health server error: %v", err)
	}
}
