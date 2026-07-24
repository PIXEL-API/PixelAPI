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
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/setup"
	"github.com/Wei-Shaw/sub2api/internal/web"

	"github.com/gin-gonic/gin"
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

const defaultMigrationTimeout = 30 * time.Minute

type commandOptions struct {
	setupMode        bool
	showVersion      bool
	migrateOnly      bool
	migrationTimeout time.Duration
	migrationThrough string
}

func (o commandOptions) validate() error {
	if o.migrateOnly && o.setupMode {
		return errors.New("--migrate-only cannot be combined with --setup")
	}
	if o.migrateOnly && o.showVersion {
		return errors.New("--migrate-only cannot be combined with --version")
	}
	if o.migrateOnly && o.migrationTimeout <= 0 {
		return errors.New("--migration-timeout must be greater than zero")
	}
	if o.migrationThrough != "" && !o.migrateOnly {
		return errors.New("--migrate-through requires --migrate-only")
	}
	if strings.ContainsAny(o.migrationThrough, `/\`) ||
		(o.migrationThrough != "" && !strings.HasSuffix(o.migrationThrough, ".sql")) {
		return errors.New("--migrate-through must be an embedded migration filename ending in .sql")
	}
	return nil
}

type bootstrapConfigLoader func() (*config.Config, error)
type configuredMigrationRunner func(context.Context, *config.Config) error

func runMigrationsOnly(
	parent context.Context,
	timeout time.Duration,
	through string,
	loadConfig bootstrapConfigLoader,
	runMigrations configuredMigrationRunner,
) error {
	if parent == nil {
		return errors.New("nil parent context")
	}
	if timeout <= 0 {
		return errors.New("migration timeout must be greater than zero")
	}
	if loadConfig == nil {
		return errors.New("nil bootstrap config loader")
	}
	if runMigrations == nil {
		return errors.New("nil configured migration runner")
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load migration config: %w", err)
	}
	if cfg == nil {
		return errors.New("load migration config: nil config")
	}
	if strings.TrimSpace(through) != "" {
		cfg.Database.MigrationThrough = strings.TrimSpace(through)
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err := runMigrations(ctx, cfg); err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}
	return nil
}

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
	migrateOnly := flag.Bool("migrate-only", false, "Run embedded database migrations and exit")
	migrationTimeout := flag.Duration("migration-timeout", defaultMigrationTimeout, "Maximum duration for --migrate-only")
	migrationThrough := flag.String("migrate-through", "", "Apply migrations through the named embedded .sql file")
	flag.Parse()

	options := commandOptions{
		setupMode:        *setupMode,
		showVersion:      *showVersion,
		migrateOnly:      *migrateOnly,
		migrationTimeout: *migrationTimeout,
		migrationThrough: strings.TrimSpace(*migrationThrough),
	}
	if err := options.validate(); err != nil {
		log.Fatalf("Invalid command options: %v", err)
	}

	if *showVersion {
		log.Printf("Sub2API %s (commit: %s, built: %s)\n", Version, Commit, Date)
		return
	}

	if *migrateOnly {
		migrationParent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runMigrationsOnly(
			migrationParent,
			*migrationTimeout,
			*migrationThrough,
			config.LoadForBootstrap,
			repository.ApplyConfiguredMigrations,
		); err != nil {
			log.Fatalf("Database migration failed: %v", err)
		}
		log.Println("Database migrations completed successfully")
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
	if err := runMainServer(); err != nil {
		if errors.Is(err, errServerRestartRequested) {
			log.Println("Graceful cleanup completed; exiting for process supervisor restart")
		} else {
			log.Printf("Server terminated: %v", err)
		}
		logger.Sync()
		os.Exit(1)
	}
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

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
		Protocols:         protocols,
	}

	if err := serveServer(server, config.ServerListenSpec{
		Network: config.ServerListenNetworkTCP,
		Address: addr,
	}); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Failed to start setup server: %v", err)
	}
}

func runMainServer() error {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := logger.Init(logger.OptionsFromConfig(cfg.Log)); err != nil {
		return fmt.Errorf("initialize logger: %w", err)
	}
	if cfg.RunMode == config.RunModeSimple {
		log.Println("⚠️  WARNING: Running in SIMPLE mode - billing and quota checks are DISABLED")
	}

	buildInfo := handler.BuildInfo{
		Version:   Version,
		BuildType: BuildType,
		Commit:    Commit,
		Date:      Date,
	}
	listenSpec, err := cfg.Server.ListenSpec()
	if err != nil {
		return fmt.Errorf("invalid server listen configuration: %w", err)
	}

	app, err := initializeApplication(buildInfo)
	if err != nil {
		return fmt.Errorf("initialize application: %w", err)
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	restartContext, stopRestart := signal.NotifyContext(context.Background(), syscall.SIGHUP)
	defer stopRestart()

	serveResults := make(chan serverServeResult, 3)
	pprofServer, pprofStartErr := startPprofServer(serveResults)

	shutdownTargets := []shutdownTarget{{
		name:    "main server",
		server:  app.Server,
		timeout: cfg.Server.HTTPDrainTimeout(),
	}}
	if pprofServer != nil {
		shutdownTargets = append(shutdownTargets, shutdownTarget{
			name:    "pprof server",
			server:  pprofServer,
			timeout: pprofShutdownTimeout,
		})
	}

	if pprofStartErr != nil {
		serveResults <- serverServeResult{name: "pprof server", err: pprofStartErr}
	} else {
		go func() {
			serveResults <- serverServeResult{
				name: "main server",
				err:  serveServer(app.Server, listenSpec),
			}
		}()
		log.Printf("Server started on %s", listenSpec.DisplayAddress())
	}

	if app.ClusterRuntime != nil && app.ClusterRuntime.Enabled() {
		go func() {
			select {
			case runtimeErr := <-app.ClusterRuntime.Fatal():
				if runtimeErr != nil {
					serveResults <- serverServeResult{
						name: "cluster runtime",
						err:  runtimeErr,
					}
				}
			case <-shutdownContext.Done():
			case <-restartContext.Done():
			}
		}()
	}

	err = runServerLifecycleWithDrain(
		shutdownContext.Done(),
		restartContext.Done(),
		serveResults,
		shutdownTargets,
		app.ClusterRuntime.BeginShutdown,
		cfg.Server.DrainDelay(),
		app.Cleanup,
		cfg.Server.CleanupTimeout(),
	)
	if err == nil {
		log.Println("Server exited")
	}
	return err
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

func startPprofServer(serveResults chan<- serverServeResult) (*http.Server, error) {
	enabledValue := strings.TrimSpace(os.Getenv("PPROF_ENABLED"))
	if enabledValue == "" {
		return nil, nil
	}

	enabled, err := strconv.ParseBool(enabledValue)
	if err != nil {
		return nil, fmt.Errorf("invalid PPROF_ENABLED value %q: %w", enabledValue, err)
	}
	if !enabled {
		return nil, nil
	}
	if serveResults == nil {
		return nil, errors.New("start pprof server: nil serve result channel")
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

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for pprof on %s: %w", addr, err)
	}
	go func() {
		serveResults <- serverServeResult{
			name: "pprof server",
			err:  server.Serve(listener),
		}
	}()

	log.Printf("pprof server started on %s", addr)
	return server, nil
}
