package main

//go:generate go run github.com/google/wire/cmd/wire

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // Admin/debug profiling is intentionally exposed only when the server is started with that route mounted.
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/setup"
	"github.com/Wei-Shaw/sub2api/internal/web"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c" //nolint:staticcheck // Keep existing h2c behavior until the server moves fully to Go's Protocols API.
)

//go:embed VERSION
var embeddedVersion string

// Build-time variables (can be set by ldflags)
var (
	Version   = ""
	Commit    = "unknown"
	Date      = "unknown"
	BuildType = "source" // "source" for manual builds, "release" for CI builds (set by ldflags)
)

func init() {
	// 如果 Version 已通过 ldflags 注入（例如 -X main.Version=...），则不要覆盖。
	if strings.TrimSpace(Version) != "" {
		return
	}

	// 默认从 embedded VERSION 文件读取版本号（编译期打包进二进制）。
	Version = strings.TrimSpace(embeddedVersion)
	if Version == "" {
		Version = "0.0.0-dev"
	}
}

// initLogger configures the default slog handler based on gin.Mode().
// In non-release mode, Debug level logs are enabled.
func main() {
	logger.InitBootstrap()
	defer logger.Sync()

	// Parse command line flags
	setupMode := flag.Bool("setup", false, "Run setup wizard in CLI mode")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		log.Printf("Sub2API %s (commit: %s, built: %s)\n", Version, Commit, Date)
		return
	}

	// CLI setup mode
	if *setupMode {
		if err := setup.RunCLI(); err != nil {
			log.Fatalf("Setup failed: %v", err)
		}
		return
	}

	// Check if setup is needed
	if setup.NeedsSetup() {
		// Check if auto-setup is enabled (for Docker deployment)
		if setup.AutoSetupEnabled() {
			log.Println("Auto setup mode enabled...")
			if err := setup.AutoSetupFromEnv(); err != nil {
				log.Fatalf("Auto setup failed: %v", err)
			}
			// Continue to main server after auto-setup
		} else {
			log.Println("First run detected, starting setup wizard...")
			runSetupServer()
			return
		}
	}

	// Normal server mode
	runMainServer()
}

func runSetupServer() {
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS(config.CORSConfig{}))
	r.Use(middleware.SecurityHeaders(config.CSPConfig{Enabled: true, Policy: config.DefaultCSPPolicy}, nil))

	// Register setup routes
	setup.RegisterRoutes(r)

	// Serve embedded frontend if available
	if web.HasEmbeddedFrontend() {
		r.Use(web.ServeEmbeddedFrontend())
	}

	// Get server address from config.yaml or environment variables (SERVER_HOST, SERVER_PORT)
	// This allows users to run setup on a different address if needed
	addr := config.GetServerAddress()
	log.Printf("Setup wizard available at http://%s", addr)
	log.Println("Complete the setup wizard to configure Sub2API")

	server := &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(r, &http2.Server{}), //nolint:staticcheck // Keep existing h2c behavior until the server moves fully to Go's Protocols API.
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if err := serveServer(server, config.ServerListenSpec{
		Network: config.ServerListenNetworkTCP,
		Address: addr,
	}); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Failed to start setup server: %v", err)
	}
}

func runMainServer() {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if err := logger.Init(logger.OptionsFromConfig(cfg.Log)); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	if cfg.RunMode == config.RunModeSimple {
		log.Println("⚠️  WARNING: Running in SIMPLE mode - billing and quota checks are DISABLED")
	}

	buildInfo := handler.BuildInfo{
		Version:   Version,
		BuildType: BuildType,
	}

	app, err := initializeApplication(buildInfo)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer app.Cleanup()

	pprofServer := startPprofServer()
	listenSpec, err := cfg.Server.ListenSpec()
	if err != nil {
		log.Fatalf("Invalid server listen configuration: %v", err)
	}

	// 启动服务器
	go func() {
		if err := serveServer(app.Server, listenSpec); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Printf("Server started on %s", listenSpec.DisplayAddress())

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := app.Server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	if pprofServer != nil {
		if err := pprofServer.Shutdown(ctx); err != nil {
			log.Fatalf("pprof server forced to shutdown: %v", err)
		}
	}

	log.Println("Server exited")
}

func serveServer(server *http.Server, spec config.ServerListenSpec) error {
	switch spec.Network {
	case config.ServerListenNetworkUnix:
		if err := os.MkdirAll(filepath.Dir(spec.Address), 0o755); err != nil {
			return fmt.Errorf("create unix socket directory: %w", err)
		}
		if err := config.RemoveUnixSocketIfExists(spec.Address); err != nil {
			return err
		}
		listener, err := net.Listen(string(spec.Network), spec.Address)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", spec.DisplayAddress(), err)
		}
		if err := os.Chmod(spec.Address, spec.Mode); err != nil {
			_ = listener.Close()
			_ = os.Remove(spec.Address)
			return fmt.Errorf("chmod unix socket %s: %w", spec.Address, err)
		}
		defer func() {
			_ = listener.Close()
			_ = os.Remove(spec.Address)
		}()
		return server.Serve(listener)
	default:
		server.Addr = spec.Address
		return server.ListenAndServe()
	}
}

func startPprofServer() *http.Server {
	enabledValue := strings.TrimSpace(os.Getenv("PPROF_ENABLED"))
	if enabledValue == "" {
		return nil
	}

	enabled, err := strconv.ParseBool(enabledValue)
	if err != nil {
		log.Fatalf("Invalid PPROF_ENABLED value %q: %v", enabledValue, err)
	}
	if !enabled {
		return nil
	}

	addr := strings.TrimSpace(os.Getenv("PPROF_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:6060"
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           http.DefaultServeMux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start pprof server on %s: %v", addr, err)
		}
	}()

	log.Printf("pprof server started on %s", addr)
	return server
}
