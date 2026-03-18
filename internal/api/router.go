package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"golang.org/x/time/rate"

	"fmt"

	"github.com/forgemill/forgemill/internal/api/handlers"
	"github.com/forgemill/forgemill/internal/api/middleware"
	"github.com/forgemill/forgemill/internal/api/ws"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/factory"
	"github.com/forgemill/forgemill/internal/service"
	versionPkg "github.com/forgemill/forgemill/internal/version"
)

type RouterConfig struct {
	DB                   *db.DB
	Auth                 *middleware.AuthMiddleware
	TargetService        *service.TargetService
	TemplateService      *service.TemplateService
	DeployService        *service.DeployService
	VMService            *service.VMService
	BlueprintService     *service.BlueprintService
	BulkDeployService    *service.BulkDeployService
	LDAPService          *service.LDAPService
	FactoryService       *service.FactoryService
	ExecutorService      *service.ExecutorService
	Hub                  *ws.Hub
	BuildHub             *factory.BuildHub
	ExecutionHub         *ws.ExecutionHub
	FrontendPath         string
	CORSOrigins          string
	WebhookService       *service.WebhookService
	AllowPrivateWebhooks bool
	TrustedProxies       string // V3-H1: Only use RealIP when behind trusted proxy
	Encryptor            interface{ Encrypt(string) (string, error); Decrypt(string) (string, error) } // V3-M14
	TLSCert              string // MED-03: Used to detect production mode for CORS enforcement
	AuditService         *service.AuditService
}

func NewRouter(cfg RouterConfig) *chi.Mux {
	r := chi.NewRouter()

	// Fix 10: Security headers on all responses
	r.Use(middleware.SecurityHeaders)
	r.Use(chimw.RequestID)
	// V3-H1: Only trust X-Forwarded-For/X-Real-IP when behind a configured trusted proxy
	if cfg.TrustedProxies != "" {
		r.Use(chimw.RealIP)
	}
	r.Use(middleware.Logging)
	r.Use(chimw.Recoverer)

	// Fix 9: Request body size limit (1MB default)
	r.Use(middleware.MaxBodySize(1 * 1024 * 1024))

	// Fix 7: Configurable CORS origins (no wildcard)
	// V3-L2: When CORSOrigins is configured, do not include broad localhost defaults
	var allowedOrigins []string
	if cfg.CORSOrigins != "" {
		allowedOrigins = strings.Split(cfg.CORSOrigins, ",")
		for i := range allowedOrigins {
			allowedOrigins[i] = strings.TrimSpace(allowedOrigins[i])
		}
	} else {
		// MED-03 fix: Log warning and refuse wildcard CORS defaults when TLS is configured
		// (indicating a production deployment). Wildcard localhost patterns combined with
		// AllowCredentials could allow any service on localhost to make authenticated requests.
		if cfg.TLSCert != "" {
			slog.Error("FORGEMILL_CORS_ORIGINS must be set when TLS is configured (production mode)")
			allowedOrigins = []string{} // deny all cross-origin in production without explicit config
		} else {
			slog.Warn("CORS origins not configured, using development defaults (localhost only)")
			allowedOrigins = []string{"http://localhost:*", "https://localhost:*"}
		}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Fix 8: Rate limiters
	// Login: 5 requests per minute per IP (5/60 = 0.083/s, burst 5)
	loginLimiter := middleware.NewRateLimiter(rate.Limit(5.0/60.0), 5)
	// Global API: 60 requests per minute per IP (1/s, burst 10)
	globalLimiter := middleware.NewRateLimiter(rate.Limit(1), 10)

	authH := handlers.NewAuthHandler(cfg.DB, cfg.Auth, cfg.AuditService)
	authH.SetLDAP(cfg.LDAPService)
	targetH := handlers.NewTargetHandler(cfg.TargetService, cfg.AuditService)
	templateH := handlers.NewTemplateHandler(cfg.TemplateService, cfg.AuditService)
	templateSourceH := handlers.NewTemplateSourceHandler(cfg.DB)
	deployH := handlers.NewDeployHandler(cfg.DeployService, cfg.AuditService)
	historyH := handlers.NewHistoryHandler(cfg.DeployService)
	settingsH := handlers.NewSettingsHandler(cfg.DB, cfg.AuditService)
	apiKeyH := handlers.NewAPIKeyHandler(cfg.DB, cfg.AuditService)
	webhookH := handlers.NewWebhookHandler(cfg.DB, cfg.AllowPrivateWebhooks, cfg.Encryptor, cfg.WebhookService, cfg.AuditService)
	auditH := handlers.NewAuditHandler(cfg.DB)
	vmH := handlers.NewVMHandler(cfg.VMService, cfg.AuditService)
	blueprintH := handlers.NewBlueprintHandler(cfg.BlueprintService)
	bulkH := handlers.NewBulkDeployHandler(cfg.BulkDeployService)
	authSourceH := handlers.NewAuthSourceHandler(cfg.LDAPService)
	factoryH := handlers.NewFactoryHandler(cfg.FactoryService, cfg.AuditService)
	actionH := handlers.NewActionHandler(cfg.DB, cfg.AuditService)
	execH := handlers.NewExecutionHandler(cfg.ExecutorService, cfg.AuditService)

	r.Route("/api", func(r chi.Router) {
		// Fix 8: Apply global rate limit to all API routes
		r.Use(globalLimiter.Limit)

		// Public version endpoint
		r.Get("/version", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"version":%q,"commit":%q,"date":%q}`, versionPkg.Version, versionPkg.Commit, versionPkg.Date)
		})

		// Public routes with login-specific rate limit
		r.Group(func(r chi.Router) {
			r.Use(loginLimiter.Limit)
			r.Post("/auth/login", authH.Login)
		})

		// WebSocket routes (JWT validated inside handler via subprotocol)
		r.Get("/ws/deploy/{id}", cfg.Hub.HandleWS)
		r.Get("/ws/build/{id}", cfg.BuildHub.HandleBuildWS)
		r.Get("/ws/execution/{id}", cfg.ExecutionHub.HandleExecutionWS)

		// Protected routes — all authenticated users (viewer+)
		r.Group(func(r chi.Router) {
			r.Use(cfg.Auth.Authenticate)

			// Auth
			r.Post("/auth/logout", authH.Logout)
			r.Get("/auth/me", authH.Me)

			// Dashboard (read-only)
			r.Get("/dashboard", settingsH.GetDashboardStats)

			// Read-only endpoints (viewer+)
			r.Get("/actions", actionH.List)
			r.Get("/targets", targetH.List)
			r.Get("/targets/types", targetH.ListTypes)
			r.Get("/targets/{id}", targetH.Get)
			r.Get("/targets/{id}/resources", targetH.GetResources)
			r.Get("/templates", templateH.List)
			r.Get("/templates/{id}", templateH.Get)
			r.Get("/templates/{id}/detail", templateH.GetDetail)
			r.Get("/templates/{id}/delete-preview", templateH.DeletePreview)
			r.Get("/template-sources", templateSourceH.List)
			r.Get("/template-sources/{id}", templateSourceH.Get)
			r.Get("/deploy/{id}", deployH.Status)
			r.Get("/deployments/{id}/actions", actionH.GetDeploymentActions)
			r.Get("/history", historyH.List)
			r.Get("/history/{id}", historyH.Detail)
			r.Get("/vms", vmH.List)
			r.Get("/vms/{id}", vmH.Get)
			r.Get("/vms/{id}/snapshots", vmH.ListSnapshots)
			r.Get("/vms/{id}/executions", execH.ListVMExecutions)
			r.Get("/executions/{id}", execH.GetExecution)
			r.Get("/blueprints", blueprintH.List)
			r.Get("/blueprints/{id}", blueprintH.Get)
			r.Get("/deploy/bulk", bulkH.List)
			r.Get("/deploy/bulk/{id}", bulkH.Get)
			r.Get("/factory/os-definitions", factoryH.ListOSDefinitions)
			r.Get("/factory/os-definitions/{id}", factoryH.GetOSDefinition)
			r.Get("/factory/prerequisites", factoryH.GetPrerequisites)
			r.Get("/factory/status", factoryH.GetStatus)
			r.Get("/factory/builds", factoryH.ListBuilds)
			r.Get("/factory/builds/{id}", factoryH.GetBuild)
			r.Get("/factory/updates", factoryH.CheckAllUpdates)
			r.Get("/factory/updates/{templateId}", factoryH.CheckTemplateUpdate)
			r.Get("/factory/schedules", factoryH.ListSchedules)
			r.Get("/factory/schedules/{id}", factoryH.GetSchedule)
			r.Get("/factory/families", factoryH.ListTemplateFamilies)
			r.Get("/factory/families/{id}/history", factoryH.GetFamilyHistory)
			r.Get("/templates/{id}/history", factoryH.GetTemplateHistory)

			// User-level endpoints (user+): deploy, blueprints, API keys, bulk, executions
			r.Group(func(r chi.Router) {
				r.Use(cfg.Auth.RequireRole("user"))

				// Action execution
				r.Post("/vms/{id}/execute", execH.Execute)
				r.Post("/executions/{id}/cancel", execH.Cancel)

				r.Post("/deploy", deployH.Deploy)
				r.Post("/deploy/{id}/cancel", deployH.Cancel)
				r.Post("/deploy/bulk", bulkH.Create)

				r.Post("/blueprints", blueprintH.Create)
				r.Put("/blueprints/{id}", blueprintH.Update)
				r.Delete("/blueprints/{id}", blueprintH.Delete)
				r.Post("/blueprints/{id}/deploy", blueprintH.Deploy)

				r.Get("/api-keys", apiKeyH.List)
				r.Post("/api-keys", apiKeyH.Create)
				r.Delete("/api-keys/{id}", apiKeyH.Delete)
			})

			// Admin-level endpoints
			r.Group(func(r chi.Router) {
				r.Use(cfg.Auth.RequireRole("admin"))

				// Actions (mutating)
				r.Post("/actions", actionH.Create)
				r.Put("/actions/{id}", actionH.Update)
				r.Delete("/actions/{id}", actionH.Delete)

				// Targets (mutating)
				r.Post("/targets", targetH.Create)
				r.Put("/targets/{id}", targetH.Update)
				r.Get("/targets/{id}/delete-preview", targetH.DeletePreview)
				r.Delete("/targets/{id}", targetH.Delete)
				r.Post("/targets/{id}/test", targetH.TestConnection)
				r.Post("/targets/{id}/sync", targetH.SyncTemplates)

				// Template Sources (mutating)
				r.Post("/template-sources", templateSourceH.Create)
				r.Put("/template-sources/{id}", templateSourceH.Update)
				r.Delete("/template-sources/{id}", templateSourceH.Delete)

				// Webhooks
				r.Get("/webhooks", webhookH.List)
				r.Post("/webhooks", webhookH.Create)
				r.Get("/webhooks/{id}", webhookH.Get)
				r.Put("/webhooks/{id}", webhookH.Update)
				r.Delete("/webhooks/{id}", webhookH.Delete)
				r.Post("/webhooks/{id}/test", webhookH.Test)

				// Managed VMs (mutating)
				r.Post("/vms", vmH.Register)
				r.Post("/vms/sync-all", vmH.SyncAll)
				r.Post("/vms/{id}/sync", vmH.SyncOne)
				r.Delete("/vms/{id}", vmH.Delete)
				r.Post("/vms/{id}/power/{action}", vmH.PowerAction)
				r.Post("/vms/{id}/snapshots", vmH.CreateSnapshot)
				r.Post("/vms/{id}/snapshots/{snapId}/revert", vmH.RevertSnapshot)
				r.Delete("/vms/{id}/snapshots/{snapId}", vmH.DeleteSnapshot)
				r.Put("/vms/{id}/resize", vmH.Resize)
				r.Get("/vms/{id}/disks", vmH.ListDisks)
				r.Put("/vms/{id}/disks/{key}/expand", vmH.ExpandDisk)
				r.Get("/vms/{id}/console", vmH.GetConsoleURL)
				r.Get("/vms/{id}/credentials", vmH.GetCredentials)
				r.Post("/vms/{id}/reset-host-key", vmH.ResetHostKey)

				// Settings
				r.Route("/settings", func(r chi.Router) {
					r.Get("/", settingsH.GetSettings)
					r.Put("/", settingsH.UpdateSettings)
				})

				// Users
				r.Get("/users", settingsH.ListUsers)
				r.Post("/users", settingsH.CreateUser)
				r.Put("/users/{id}/password", settingsH.ChangePassword)
				r.Put("/users/{id}/role", settingsH.UpdateUserRole)
				r.Delete("/users/{id}", settingsH.DeleteUser)
				r.Delete("/deployment-history", settingsH.ClearDeploymentHistory)

				// Audit log
				r.Get("/audit-logs", auditH.List)

				// Auth Sources
				r.Get("/auth-sources", authSourceH.List)
				r.Post("/auth-sources", authSourceH.Create)
				r.Get("/auth-sources/{id}", authSourceH.Get)
				r.Put("/auth-sources/{id}", authSourceH.Update)
				r.Delete("/auth-sources/{id}", authSourceH.Delete)
				r.Post("/auth-sources/{id}/test", authSourceH.TestConnection)

				// Factory (mutating)
				r.Post("/factory/builds", factoryH.StartBuild)
				r.Post("/factory/builds/{id}/cancel", factoryH.CancelBuild)
				r.Delete("/factory/builds/{id}", factoryH.DeleteBuild)
				r.Get("/factory/builds/{id}/hcl", factoryH.GetBuildHCL)
				r.Post("/factory/updates/{templateId}/rebuild", factoryH.RebuildTemplate)
				r.Post("/factory/schedules", factoryH.CreateSchedule)
				r.Put("/factory/schedules/{id}", factoryH.UpdateSchedule)
				r.Delete("/factory/schedules/{id}", factoryH.DeleteSchedule)
				r.Post("/templates/{id}/cleanup", factoryH.CleanupSuperseded)
				r.Delete("/templates/{id}", templateH.Delete)
			})
		})
	})

	// Serve frontend static files
	serveFrontend(r, cfg.FrontendPath)

	return r
}

func serveFrontend(r *chi.Mux, frontendPath string) {
	absPath, err := filepath.Abs(frontendPath)
	if err != nil {
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return
	}

	fsys := os.DirFS(absPath)
	fileServer := http.FileServer(http.FS(fsys))

	// 2.16: Read index.html once at startup instead of on every SPA fallback request
	indexHTML, indexErr := fs.ReadFile(fsys, "index.html")
	if indexErr != nil {
		slog.Warn("frontend index.html not found", "path", absPath)
	}

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		if f, err := fs.Stat(fsys, path); err == nil && !f.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve cached index.html
		if indexErr != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexHTML)
	})
}
