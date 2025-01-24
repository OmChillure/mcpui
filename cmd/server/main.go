package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/MegaGrindStone/go-mcp"
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
	llm, err := cfg.LLM.llm()
	if err != nil {
		panic(err)
	}

	dbPath := filepath.Join(cfgDir, "/mcpwebui/store.db")
	boltDB, err := services.NewBoltDB(dbPath)
	if err != nil {
		panic(err)
	}

	mcpClientInfo := mcp.Info{
		Name:    "mcp-web-ui",
		Version: "0.1.0",
	}

	mcpClients, stdIOCmds := populateMCPClients(cfg, mcpClientInfo)

	var mcpCancels []context.CancelFunc
	for i, cli := range mcpClients {
		log.Printf("Connecting to MCP server at index %d", i)

		connectCtx, connectCancel := context.WithCancel(context.Background())
		mcpCancels = append(mcpCancels, connectCancel)

		ready := make(chan struct{})
		errs := make(chan error, 1)

		go func() {
			if err := cli.Connect(connectCtx, ready); err != nil {
				errs <- err
			}
		}()

		select {
		case err := <-errs:
			panic(err)
		case <-ready:
		}

		mcpClients[i] = cli

		log.Printf("Connected to MCP server %s", mcpClients[i].ServerInfo().Name)
	}

	m, err := handlers.NewMain(llm, boltDB, mcpClients)
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
		for _, cancel := range mcpCancels {
			cancel()
		}
		for _, stdIOCmd := range stdIOCmds {
			if err := stdIOCmd.Wait(); err != nil {
				log.Printf("Failed to wait for stdIO command: %v", err)
			}
		}

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

func populateMCPClients(cfg config, mcpClientInfo mcp.Info) ([]*mcp.Client, []*exec.Cmd) {
	var mcpClients []*mcp.Client

	for _, mcpSSEServerConfig := range cfg.MCPSSEServers {
		sseClient := mcp.NewSSEClient(mcpSSEServerConfig.URL, nil)
		cli := mcp.NewClient(mcpClientInfo, sseClient)
		mcpClients = append(mcpClients, cli)
	}

	var stdIOCmds []*exec.Cmd
	for _, mcpStdIOServerConfig := range cfg.MCPStdIOServers {
		cmd := exec.Command(mcpStdIOServerConfig.Command, mcpStdIOServerConfig.Args...)
		stdIOCmds = append(stdIOCmds, cmd)

		in, err := cmd.StdinPipe()
		if err != nil {
			panic(err)
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			panic(err)
		}
		if err := cmd.Start(); err != nil {
			panic(err)
		}

		cliStdIO := mcp.NewStdIO(out, in)

		cli := mcp.NewClient(mcpClientInfo, cliStdIO)
		mcpClients = append(mcpClients, cli)
	}

	return mcpClients, stdIOCmds
}
