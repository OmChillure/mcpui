package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	mcpwebui "github.com/MegaGrindStone/mcp-web-ui"
	"github.com/MegaGrindStone/mcp-web-ui/internal/handlers"
	"github.com/MegaGrindStone/mcp-web-ui/internal/services"
	"gopkg.in/yaml.v3"
)

func main() {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(fmt.Errorf("error getting user config dir: %w", err))
	}
	cfgPath := filepath.Join(cfgDir, "/mcpwebui")
	if err := os.MkdirAll(cfgPath, 0755); err != nil {
		log.Fatal(fmt.Errorf("error creating config directory: %w", err))
	}

	cfgFilePath := filepath.Join(cfgDir, "/mcpwebui/config.yaml")
	cfgFile, err := os.Open(cfgFilePath)
	if err != nil {
		log.Fatal(fmt.Errorf("error opening config file: %w", err))
	}
	defer cfgFile.Close()

	cfg := config{}
	if err := yaml.NewDecoder(cfgFile).Decode(&cfg); err != nil {
		panic(fmt.Errorf("error decoding config file: %w", err))
	}
	llm, err := cfg.LLM.LLM()
	if err != nil {
		panic(err)
	}

	dbPath := filepath.Join(cfgDir, "/mcpwebui/store.db")
	boltDB, err := services.NewBoltDB(dbPath)
	if err != nil {
		panic(err)
	}

	m, err := handlers.NewMain(llm, boltDB)
	if err != nil {
		panic(err)
	}

	// Serve static files
	staticFS, err := fs.Sub(mcpwebui.StaticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(staticFS))

	// Create custom mux
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	mux.HandleFunc("/", m.HandleHome)
	mux.HandleFunc("/chats", m.HandleChats)
	mux.HandleFunc("/sse/messages", m.HandleSSE)
	mux.HandleFunc("/sse/chats", m.HandleSSE)

	// Create custom server
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	srv.RegisterOnShutdown(func() {
		if err := m.Shutdown(context.Background()); err != nil {
			log.Printf("Failed to shutdown sse server: %v", err)
		}
	})

	// Channel to listen for errors coming from the listener
	serverErrors := make(chan error, 1)

	// Start server in goroutine
	go func() {
		log.Println("Server starting on :" + cfg.Port)
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
