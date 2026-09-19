package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/mgmt"
	"github.com/tobilg/neoserver/internal/observability"
	"github.com/tobilg/neoserver/internal/server"
	"github.com/tobilg/neoserver/internal/store"
	"go.uber.org/automaxprocs/maxprocs"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		cmdInit(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "create-token":
		cmdCreateToken(os.Args[2:])
	case "rotate-signing-key":
		cmdRotateSigningKey(os.Args[2:])
	case "add-claim-mapping":
		cmdAddClaimMapping(os.Args[2:])
	case "openapi-dump":
		cmdOpenAPIDump(os.Args[2:])
	case "version", "-version", "--version":
		fmt.Printf("%s %s (%s)\n", conf.App.Name, conf.App.Version, conf.App.Commit)
	case "help", "-h", "--help":
		printUsage()
	default:
		// Backwards compatibility: if no subcommand, assume "serve"
		cmdServe(os.Args[1:])
	}
}

func printUsage() {
	fmt.Printf(`%s - OGC API Features / WMS / WFS server for geospatial data

Usage:
  %s <command> [options]

Commands:
  init               Initialize a new backing store with encryption
  serve              Start the server (default if no command given)
  create-token       Create a new self-signed access token
  rotate-signing-key Rotate the internal signing key (invalidates all tokens)
  add-claim-mapping  Add an OIDC/JWT claim to role mapping
  openapi-dump       Write the management OpenAPI document to stdout
  version            Print version and exit
  help               Show this help message

Run '%s <command> -h' for more information on a specific command.
`, conf.App.Name, conf.App.Name, conf.App.Name)
}

func cmdOpenAPIDump(args []string) {
	fs := flag.NewFlagSet("openapi-dump", flag.ExitOnError)
	basePath := fs.String("base-path", "", "Base path to include in the server URL")
	urlBase := fs.String("url-base", "http://localhost:9000", "Public server URL")
	outputPath := fs.String("output", "", "Write to a file instead of stdout")
	_ = fs.Parse(args)
	cfg := conf.Config{Server: conf.Server{BasePath: *basePath, UrlBase: *urlBase}}
	document := mgmt.BuildOpenAPI(cfg)
	output := io.Writer(os.Stdout)
	var file *os.File
	if *outputPath != "" {
		var err error
		file, err = os.Create(*outputPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error creating OpenAPI output:", err)
			os.Exit(1)
		}
		defer file.Close()
		output = file
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		fmt.Fprintln(os.Stderr, "Error encoding OpenAPI document:", err)
		os.Exit(1)
	}
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	storePath := fs.String("store-path", "", "Override the configured backing store path")
	configPath := fs.String("config", "", "Configuration file (same defaults as serve)")
	fs.Parse(args)
	configuredStore := mustStoreConfig(*configPath, *storePath)
	*storePath = configuredStore.Path

	// Get encryption key from environment
	encryptionKey := configuredStore.EncryptionKey
	if encryptionKey == "" {
		fmt.Println("Error: NEOSRV_STORE_KEY environment variable is required")
		fmt.Println("Generate a key with: openssl rand -hex 32")
		os.Exit(1)
	}

	// Check if store already exists
	if _, err := os.Stat(*storePath); err == nil {
		fmt.Printf("Error: Store already exists at %s\n", *storePath)
		fmt.Println("Delete the file first if you want to reinitialize.")
		os.Exit(1)
	}

	// Create parent directory if needed
	parent := filepath.Dir(*storePath)
	if err := os.MkdirAll(parent, 0755); err != nil {
		fmt.Printf("Error creating data directory: %v\n", err)
		os.Exit(1)
	}

	// Initialize the store
	cfg := store.Config{
		Path:          *storePath,
		EncryptionKey: encryptionKey,
	}

	s, bootstrapToken, err := store.Init(cfg)
	if err != nil {
		fmt.Printf("Error initializing store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	fmt.Printf("Backing store initialized at %s\n", *storePath)
	fmt.Println("Internal signing key generated and stored.")
	fmt.Println()
	fmt.Println("Bootstrap Access Token (super_admin, expires in 24h):")
	fmt.Println(bootstrapToken)
	fmt.Println()
	fmt.Println("IMPORTANT: Save this token securely. Use it to configure OIDC claim mappings.")
	fmt.Printf("Generate a new token with: neoserver create-token --store-path '%s' --role super_admin\n", strings.ReplaceAll(*storePath, "'", "'\"'\"'"))
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to TOML config file (optional)")
	debug := fs.Bool("debug", false, "Enable debug logging")
	devel := fs.Bool("devel", false, "Development mode (e.g. reload templates)")
	takeoverOwner := fs.String("take-over-tile-cache-owner", "", "Replace the exact S3 tile-cache owner after confirming its process is dead")
	fs.Parse(args)
	if *takeoverOwner != "" {
		if _, err := uuid.Parse(*takeoverOwner); err != nil {
			fmt.Fprintln(os.Stderr, "Error: --take-over-tile-cache-owner must be the UUID reported by the blocked startup")
			os.Exit(2)
		}
	}

	_, _ = maxprocs.Set(maxprocs.Logger(func(format string, args ...any) {}))

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: func() slog.Level {
			if *debug {
				return slog.LevelDebug
			}
			return slog.LevelInfo
		}(),
	}))
	slog.SetDefault(logger)

	cfg, err := conf.Load(*configPath, *debug, *devel)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	var shutdownOTel observability.Shutdown
	if cfg.Observability.OTel.Enabled {
		shutdownOTel, err = observability.InitOTLP(context.Background(), cfg.Observability.OTel.ServiceName)
		if err != nil {
			logger.Warn("OpenTelemetry initialization failed; continuing without export", "error", err)
		}
	}
	if shutdownOTel != nil {
		defer func() {
			timeout := time.Duration(cfg.Observability.OTel.ShutdownTimeoutSec) * time.Second
			if timeout <= 0 {
				timeout = 5 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			_ = shutdownOTel(ctx)
		}()
	}

	// Validate encryption key BEFORE any store operations
	encryptionKey := cfg.Store.EncryptionKey
	if encryptionKey == "" {
		slog.Error("NEOSRV_STORE_KEY environment variable is required")
		slog.Error("Generate one with: openssl rand -hex 32")
		os.Exit(1)
	}

	storePath := cfg.Store.Path
	storeCfg := store.Config{
		Path:          storePath,
		EncryptionKey: encryptionKey,
	}

	// Serving never creates security state implicitly. Initialization is an
	// explicit operator action so the bootstrap token is only printed by init.
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		slog.Error("backing store does not exist; run 'neoserver init' first", "path", storePath)
		os.Exit(1)
	} else if err != nil {
		slog.Error("failed to inspect backing store", "path", storePath, "error", err)
		os.Exit(1)
	}
	s, err := store.Open(storeCfg)
	if err != nil {
		slog.Error("failed to open backing store", "error", err)
		os.Exit(1)
	}
	logger.Info("backing store opened", "path", storePath)
	defer s.Close()

	srv, err := server.New(context.Background(), cfg, logger, s, server.WithTileCacheTakeoverOwner(*takeoverOwner))
	if err != nil {
		slog.Error("failed to initialize server", "err", err)
		os.Exit(1)
	}
	if shutdownOTel != nil {
		if err := observability.RegisterCacheMetrics(srv.Cache()); err != nil {
			logger.Warn("cache metric registration failed", "error", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Start(); err != nil {
			slog.Error("server exited with error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "err", err)
		os.Exit(1)
	}
}

func cmdCreateToken(args []string) {
	fs := flag.NewFlagSet("create-token", flag.ExitOnError)
	storePath := fs.String("store-path", "", "Override the configured backing store path")
	configPath := fs.String("config", "", "Configuration file (same defaults as serve)")
	role := fs.String("role", "super_admin", "Role for the token (super_admin, admin, editor, viewer)")
	subject := fs.String("subject", "admin", "Subject identifier for the token")
	expires := fs.String("expires", "24h", "Token expiration duration (e.g., 24h, 7d)")
	fs.Parse(args)
	configuredStore := mustStoreConfig(*configPath, *storePath)
	*storePath = configuredStore.Path

	// Get encryption key from environment
	encryptionKey := configuredStore.EncryptionKey
	if encryptionKey == "" {
		fmt.Println("Error: NEOSRV_STORE_KEY environment variable is required")
		os.Exit(1)
	}

	duration, err := tokenDuration(*expires)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	if !validTokenRole(*role) {
		fmt.Fprintln(os.Stderr, "Error: role must be super_admin, admin, editor, or viewer")
		os.Exit(1)
	}

	// Open the store
	cfg := store.Config{
		Path:          *storePath,
		EncryptionKey: encryptionKey,
	}

	s, err := store.Open(cfg)
	if err != nil {
		fmt.Printf("Error opening store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Get the active signing key
	signingKey, err := s.GetActiveSigningKey(context.Background())
	if err != nil {
		fmt.Printf("Error getting signing key: %v\n", err)
		os.Exit(1)
	}

	// Create the token
	token, err := s.CreateToken(signingKey, *subject, *role, duration)
	if err != nil {
		fmt.Printf("Error creating token: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Access Token (%s, expires in %s):\n", *role, *expires)
	fmt.Println(token)
	fmt.Println()
	fmt.Println("Use with: Authorization: Bearer <token>")
}

func cmdRotateSigningKey(args []string) {
	fs := flag.NewFlagSet("rotate-signing-key", flag.ExitOnError)
	storePath := fs.String("store-path", "", "Override the configured backing store path")
	configPath := fs.String("config", "", "Configuration file (same defaults as serve)")
	force := fs.Bool("force", false, "Skip confirmation prompt")
	fs.Parse(args)
	configuredStore := mustStoreConfig(*configPath, *storePath)
	*storePath = configuredStore.Path

	// Get encryption key from environment
	encryptionKey := configuredStore.EncryptionKey
	if encryptionKey == "" {
		fmt.Println("Error: NEOSRV_STORE_KEY environment variable is required")
		os.Exit(1)
	}

	if !*force {
		fmt.Println("WARNING: This will invalidate all self-signed tokens!")
		fmt.Print("Are you sure you want to continue? (y/N): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Aborted.")
			os.Exit(0)
		}
	}

	// Open the store
	cfg := store.Config{
		Path:          *storePath,
		EncryptionKey: encryptionKey,
	}

	s, err := store.Open(cfg)
	if err != nil {
		fmt.Printf("Error opening store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Rotate the signing key
	_, err = s.RotateSigningKey(context.Background())
	if err != nil {
		fmt.Printf("Error rotating signing key: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("New signing key generated. All previous self-signed tokens are now invalid.")
}

func cmdAddClaimMapping(args []string) {
	fs := flag.NewFlagSet("add-claim-mapping", flag.ExitOnError)
	storePath := fs.String("store-path", "", "Override the configured backing store path")
	configPath := fs.String("config", "", "Configuration file (same defaults as serve)")
	workspace := fs.String("workspace", "*", "Workspace ID (* for global)")
	claimName := fs.String("claim", "", "Claim name (e.g., groups, roles)")
	claimValue := fs.String("value", "", "Claim value to match")
	role := fs.String("role", "", "Role to assign (super_admin, admin, editor, viewer)")
	priority := fs.Int("priority", 0, "Priority (higher = checked first)")
	fs.Parse(args)
	configuredStore := mustStoreConfig(*configPath, *storePath)
	*storePath = configuredStore.Path

	if *claimName == "" || *claimValue == "" || *role == "" {
		fmt.Println("Error: --claim, --value, and --role are required")
		fs.PrintDefaults()
		os.Exit(1)
	}

	// Validate role
	validRoles := map[string]bool{"super_admin": true, "admin": true, "editor": true, "viewer": true}
	if !validRoles[*role] {
		fmt.Printf("Error: invalid role: %s (must be super_admin, admin, editor, or viewer)\n", *role)
		os.Exit(1)
	}

	// Get encryption key from environment
	encryptionKey := configuredStore.EncryptionKey
	if encryptionKey == "" {
		fmt.Println("Error: NEOSRV_STORE_KEY environment variable is required")
		os.Exit(1)
	}

	// Open the store
	cfg := store.Config{
		Path:          *storePath,
		EncryptionKey: encryptionKey,
	}

	s, err := store.Open(cfg)
	if err != nil {
		fmt.Printf("Error opening store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	// Create the claim mapping
	input := store.CreateClaimMappingInput{
		WorkspaceID: *workspace,
		ClaimName:   *claimName,
		ClaimValue:  *claimValue,
		RoleID:      *role,
		Priority:    *priority,
	}

	mapping, err := s.CreateClaimMapping(context.Background(), input)
	if err != nil {
		fmt.Printf("Error creating claim mapping: %v\n", err)
		os.Exit(1)
	}

	scope := "Global"
	if *workspace != "*" {
		scope = fmt.Sprintf("Workspace '%s'", *workspace)
	}

	fmt.Printf("%s claim mapping created:\n", scope)
	fmt.Printf("  ID: %s\n", mapping.ID)
	fmt.Printf("  Claim: %s = \"%s\" → %s\n", *claimName, *claimValue, *role)
	fmt.Println()
	fmt.Println("Users with this claim can now authenticate with this role via OIDC/JWT.")
}
