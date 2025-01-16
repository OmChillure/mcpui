package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcpwebui "github.com/MegaGrindStone/mcp-web-ui"
	"github.com/MegaGrindStone/mcp-web-ui/internal/handlers"
)

func main() {
	h, err := handlers.NewHome()
	if err != nil {
		log.Fatal(err)
	}

	// Serve static files
	staticFS, err := fs.Sub(mcpwebui.StaticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(staticFS))

	// Create custom mux
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	mux.HandleFunc("/", h.HandleHome)
	mux.HandleFunc("/chats", h.HandleChats)
	mux.HandleFunc("/sse/messages", h.HandleSSE)
	mux.HandleFunc("/sse/chats", h.HandleSSE)

	// Create custom server
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	srv.RegisterOnShutdown(func() {
		if err := h.Shutdown(context.Background()); err != nil {
			log.Printf("Failed to shutdown sse server: %v", err)
		}
	})

	// Channel to listen for errors coming from the listener
	serverErrors := make(chan error, 1)

	// Start server in goroutine
	go func() {
		log.Println("Server starting on :8080")
		serverErrors <- srv.ListenAndServe()
	}()

	// Channel to listen for interrupt/terminate signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Blocking select waiting for either interrupt or server error
	select {
	case err := <-serverErrors:
		log.Printf("Server error: %v", err)

	case sig := <-shutdown:
		log.Printf("Start shutdown, signal: %v", sig)

		// Create context with timeout for shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Gracefully shutdown the server
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Graceful shutdown failed: %v", err)
			if err := srv.Close(); err != nil {
				log.Printf("Forcing server close: %v", err)
			}
		}
	}
}
