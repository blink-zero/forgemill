package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/forgemill/forgemill/internal/api"
	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/api/ws"
	"github.com/forgemill/forgemill/internal/config"
	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
	"github.com/forgemill/forgemill/internal/factory"
	"github.com/forgemill/forgemill/internal/provider"
	// Import provider packages to trigger their init() registration
	_ "github.com/forgemill/forgemill/internal/provider/proxmox"
	_ "github.com/forgemill/forgemill/internal/provider/vmware"
	"github.com/forgemill/forgemill/internal/service"
	"github.com/forgemill/forgemill/internal/version"
)

func main() {
	cfg := config.Load()

	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	slog.Info("starting Forgemill", "version", version.Version, "commit", version.Commit, "built", version.Date)

	// Validate provider registry (informational - fallback exists if registration fails)
	validateProviderRegistry()

	// Fix 1: Auto-generate secrets if defaults are still in use
	secretsFile := filepath.Join(cfg.DataDir, ".secrets")
	cfg.JWTSecret = ensureSecret("FORGEMILL_JWT_SECRET", config.DefaultJWTSecret, secretsFile, "jwt_secret", cfg.JWTSecret)
	cfg.EncryptionKey = ensureSecret("FORGEMILL_ENCRYPTION_KEY", config.DefaultEncryptionKey, secretsFile, "encryption_key", cfg.EncryptionKey)

	slog.Info("starting forgemill", "listen", cfg.ListenAddr)

	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	enc, err := crypto.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		slog.Error("failed to create encryptor", "error", err)
		os.Exit(1)
	}

	if err := ensureAdminUser(database, cfg); err != nil {
		slog.Error("failed to ensure admin user", "error", err)
		os.Exit(1)
	}

	hub := ws.NewHub()
	hub.SetJWTSecret(cfg.JWTSecret)
	// V3-H2: Wire user store for full WebSocket auth validation
	hub.SetUserStore(database)
	buildHub := factory.NewBuildHub()
	buildHub.SetJWTSecret(cfg.JWTSecret)
	// V3-H2: Wire user store for full WebSocket auth validation
	buildHub.SetUserStore(database)
	executionHub := ws.NewExecutionHub()
	executionHub.SetJWTSecret(cfg.JWTSecret)
	executionHub.SetUserStore(database)
	// MED-06: NewAuthMiddleware now returns an error if JWT secret is too short
	auth, err := middleware.NewAuthMiddleware(database, cfg.JWTSecret, cfg.JWTExpiry)
	if err != nil {
		slog.Error("failed to initialize auth middleware", "error", err)
		os.Exit(1)
	}
	targetSvc := service.NewTargetService(database, enc)
	templateSvc := service.NewTemplateService(database, targetSvc)
	webhookSvc := service.NewWebhookService(database, cfg.AllowPrivateWebhooks)
	webhookSvc.SetDecryptor(enc) // V3-M14: Wire decryptor for HMAC signing with encrypted secrets
	deploySvc := service.NewDeployService(database, targetSvc, hub, webhookSvc, enc)
	vmSvc := service.NewVMService(database, targetSvc, enc)
	executorSvc := service.NewExecutorService(database, targetSvc, enc, executionHub)
	// Auto-sync VMs after deployments complete
	deploySvc.SetOnDeployComplete(func() {
		go func() {
			time.Sleep(5 * time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if _, err := vmSvc.SyncAll(ctx); err != nil {
				slog.Warn("auto-sync after deploy failed", "error", err)
			} else {
				slog.Info("auto-sync completed after deployment")
			}
		}()
	})
	blueprintSvc := service.NewBlueprintService(database, deploySvc)
	bulkDeploySvc := service.NewBulkDeployService(database, deploySvc)
	ldapSvc := service.NewLDAPService(database, enc)
	buildEngine := factory.NewEngine(database, buildHub)
	if n, err := database.CleanupStaleBuilds(); err != nil {
		slog.Error("failed to cleanup stale builds", "error", err)
	} else if n > 0 {
		slog.Info("cleaned up stale builds from previous run", "count", n)
	}
	auditSvc := service.NewAuditService(database)
	auditSvc.StartRetentionCleanup()
	factorySvc := service.NewFactoryService(database, buildEngine, enc)
	factorySvc.SetWebhookService(webhookSvc)
	factorySvc.SetTemplateSyncCallback(func(ctx context.Context, targetID int64) {
		if _, err := targetSvc.SyncTemplates(ctx, targetID); err != nil {
			slog.Error("auto-sync templates after build failed", "target_id", targetID, "error", err)
		}
	})

	router := api.NewRouter(api.RouterConfig{
		DB:                database,
		Auth:              auth,
		TargetService:     targetSvc,
		TemplateService:   templateSvc,
		DeployService:     deploySvc,
		VMService:         vmSvc,
		BlueprintService:  blueprintSvc,
		BulkDeployService: bulkDeploySvc,
		LDAPService:       ldapSvc,
		FactoryService:    factorySvc,
		AuditService:      auditSvc,
		ExecutorService:   executorSvc,
		Hub:               hub,
		BuildHub:          buildHub,
		ExecutionHub:      executionHub,
		FrontendPath:      cfg.FrontendPath,
		CORSOrigins:       cfg.CORSOrigins,
		WebhookService:       webhookSvc,
		AllowPrivateWebhooks: cfg.AllowPrivateWebhooks,
		TrustedProxies:    cfg.TrustedProxies, // V3-H1
		Encryptor:         enc,                 // V3-M14
		TLSCert:           cfg.TLSCert,         // MED-03: detect production mode for CORS
	})

	// Start the template build scheduler
	buildScheduler := factory.NewBuildScheduler(database, factorySvc.GetEngine(), factorySvc.GetUpdateChecker(), webhookSvc)
	buildScheduler.SetRebuildFunc(func(templateID int64, userID int64) error {
		_, err := factorySvc.RebuildTemplate(templateID, userID)
		return err
	})
	buildScheduler.Start()

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("shutting down", "signal", sig)
		buildScheduler.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	// V3-M8: Support TLS when cert and key are configured
	slog.Info("server ready", "addr", cfg.ListenAddr)
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		slog.Info("TLS enabled", "cert", cfg.TLSCert, "key", cfg.TLSKey)
		if err := server.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	} else {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}
}

// ensureSecret checks if a secret is still set to the insecure default.
// If so, it attempts to load from the secrets file or generates a new random secret.
func ensureSecret(envKey, defaultVal, secretsFile, fieldName, currentVal string) string {
	// MED-01: Also reject empty string as a valid secret
	if currentVal != defaultVal && currentVal != "" {
		return currentVal
	}

	slog.Warn("insecure default detected, auto-generating secret", "field", fieldName)

	// Try loading from existing secrets file
	if data, err := os.ReadFile(secretsFile); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && parts[0] == fieldName {
				// F-1: Validate loaded secret is non-empty after trimming
				val := strings.TrimSpace(parts[1])
				if val == "" {
					slog.Warn("empty secret found in file, regenerating", "field", fieldName)
					break
				}
				slog.Info("loaded secret from file", "field", fieldName, "path", secretsFile)
				return val
			}
		}
	}

	// Generate a new random 32-byte secret
	secret := generateRandomHex(32)

	// Save to secrets file (append or create)
	if err := os.MkdirAll(filepath.Dir(secretsFile), 0700); err != nil {
		slog.Error("failed to create secrets directory", "error", err)
		return secret
	}

	// Read existing content to append
	existing := ""
	if data, err := os.ReadFile(secretsFile); err == nil {
		existing = string(data)
		if !strings.HasSuffix(existing, "\n") {
			existing += "\n"
		}
	}

	content := existing + fieldName + "=" + secret + "\n"
	if err := os.WriteFile(secretsFile, []byte(content), 0600); err != nil {
		slog.Error("failed to write secrets file", "error", err)
	} else {
		slog.Info("saved auto-generated secret", "field", fieldName, "path", secretsFile)
	}

	return secret
}

func generateRandomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// generateRandomPassword creates a random password of the given length
// using alphanumeric and select special characters.
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic("failed to generate random password: " + err.Error())
		}
		result[i] = charset[n.Int64()]
	}
	return string(result)
}

func ensureAdminUser(database *db.DB, cfg *config.Config) error {
	count, err := database.UserCount()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	password := cfg.AdminPassword
	if password == "" {
		// Fix 2: Generate a random admin password instead of using "admin"
		password = generateRandomPassword(16)
		// V3-H10: Print to stderr instead of structured log to avoid log aggregation capture
		fmt.Fprintf(os.Stderr, "\n!!! INITIAL ADMIN PASSWORD: %s !!!\n"+
			"!!! Change this immediately. This will not be shown again. !!!\n\n", password)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user := &models.User{
		Username:     cfg.AdminUsername,
		PasswordHash: string(hash),
		DisplayName:  "Administrator",
		Role:         "admin",
		IsActive:     true,
	}

	slog.Info("creating admin user", "username", cfg.AdminUsername)
	return database.CreateUser(user)
}

// validateProviderRegistry checks that expected providers and metadata are registered.
func validateProviderRegistry() {
	expected := []string{"vcenter", "esxi", "proxmox"}
	registered := provider.RegisteredTypes()
	metadata := provider.GetAllMetadata()

	// Build set of types with metadata
	metadataTypes := make(map[string]bool)
	for _, m := range metadata {
		metadataTypes[m.ID] = true
	}

	// Check all expected types are registered with both provider and metadata
	for _, e := range expected {
		hasProvider := false
		for _, r := range registered {
			if r == e {
				hasProvider = true
				break
			}
		}
		hasMetadata := metadataTypes[e]

		if !hasProvider {
			slog.Error("provider not registered", "type", e)
		}
		if !hasMetadata {
			slog.Error("provider metadata not registered", "type", e)
		}
	}

	slog.Info("provider registry validated", "registered_types", registered, "metadata_count", len(metadata))
}
