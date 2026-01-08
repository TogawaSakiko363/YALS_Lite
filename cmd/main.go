package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"YALS/internal/config"
	"YALS/internal/executor"
	"YALS/internal/handler"
	"YALS/internal/logger"
	"YALS/internal/utils"
)

func main() {
	configFile := flag.String("c", "config.yaml", "Path to configuration file")
	webDir := flag.String("w", "./web", "Path to web frontend directory")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s\n%s\n", utils.GetAppName(), utils.GetVersionInfo())
		os.Exit(0)
	}

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	setupLogging(cfg)

	if _, err := os.Stat(*webDir); os.IsNotExist(err) {
		logger.Warnf("Web directory '%s' does not exist", *webDir)
	} else {
		logger.Infof("Using web directory: %s", *webDir)
	}

	serverInfo := config.NewServerInfo(cfg)
	cmdExecutor := executor.NewExecutor(cfg)

	pingInterval := time.Duration(30) * time.Second
	pongWait := time.Duration(60) * time.Second
	h := handler.NewHandler(serverInfo, cmdExecutor, pingInterval, pongWait)

	mux := http.NewServeMux()
	h.SetupRoutes(mux, *webDir)

	addr := fmt.Sprintf("%s:%d", cfg.Listen.Host, cfg.Listen.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if cfg.Listen.TLS {
			if _, err := os.Stat(cfg.Listen.TLSCertFile); os.IsNotExist(err) {
				logger.Fatalf("TLS certificate file not found: %s", cfg.Listen.TLSCertFile)
			}
			if _, err := os.Stat(cfg.Listen.TLSKeyFile); os.IsNotExist(err) {
				logger.Fatalf("TLS key file not found: %s", cfg.Listen.TLSKeyFile)
			}

			logger.Infof("Starting HTTPS server on %s", addr)

			if err := httpServer.ListenAndServeTLS(cfg.Listen.TLSCertFile, cfg.Listen.TLSKeyFile); err != nil && err != http.ErrServerClosed {
				logger.Fatalf("Failed to start HTTPS server: %v", err)
			}
		} else {
			logger.Infof("Starting HTTP server on %s", addr)
			logger.Warnf("TLS is disabled. Consider enabling TLS for production use.")

			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Fatalf("Failed to start HTTP server: %v", err)
			}
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	logger.Infof("YALS started successfully")

	<-stop
	logger.Info("Shutting down server...")
}

func setupLogging(cfg *config.Config) {
	logger.SetGlobalLevelFromString(cfg.Listen.LogLevel)
	logger.Debugf("Logging level set to: %s", cfg.Listen.LogLevel)

	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}
