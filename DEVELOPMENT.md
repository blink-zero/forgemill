# Forgemill Development Guide

> **THE DEFINITIVE REFERENCE** for human developers and AI agents working on Forgemill.
> Read this document completely before making any code changes.

---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [Development Environment Setup](#2-development-environment-setup)
3. [Architecture Deep Dive](#3-architecture-deep-dive)
4. [Coding Standards & Conventions](#4-coding-standards--conventions)
5. [Database & Migrations](#5-database--migrations)
6. [Testing](#6-testing)
7. [Security Standards (Non-Negotiable)](#7-security-standards-non-negotiable)
8. [Adding New Features](#8-adding-new-features)
9. [Docker & Deployment](#9-docker--deployment)
10. [Pull Request & Contribution Standards](#10-pull-request--contribution-standards)
11. [AI Agent Guidelines](#11-ai-agent-guidelines-critical)

---

## 1. Project Overview

### What Forgemill Is

Forgemill is a self-hosted VM lifecycle management platform for VMware vSphere, Proxmox VE, and ESXi. It provides:

- **Template-based VM deployment** with cloud-init customization
- **Template Factory** automated Packer-based template builds from ISOs
- **VM lifecycle management** power, snapshots, resize, console access
- **Post-deploy automation** via SSH with parameterized action scripts
- **Blueprints** for reusable deployment configurations
- **Bulk deployment** for multi-VM provisioning
- **Real-time progress** via WebSocket streaming
- **RBAC** with local, LDAP, and API key authentication

### Technology Stack

| Layer | Technology | Version |
|-------|-----------|---------|
| Backend | Go | 1.24+ |
| HTTP Router | chi/v5 | 5.2.5 |
| WebSocket | gorilla/websocket | 1.5.3 |
| Database | SQLite (modernc.org/sqlite) | WAL mode |
| Auth | JWT (HS256), bcrypt, LDAP | |
| Encryption | AES-256-GCM + HKDF-SHA256 | |
| Frontend | React 18, TypeScript 5.7 | |
| Build Tool | Vite 6 | |
| CSS | Tailwind CSS | |
| UI Components | Radix UI | |
| CLI | spf13/cobra | |
| VMware SDK | govmomi | 0.53.0 |
| Template Builds | HashiCorp Packer | |

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Frontend (React/TypeScript)                     │
│            Vite  ·  Tailwind CSS  ·  Radix UI  ·  React Router       │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │ REST API + WebSocket
┌─────────────────────────────────┴───────────────────────────────────┐
│                         Backend (Go 1.24)                            │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                      API Layer (chi router)                   │   │
│  │   Handlers  ·  Middleware (auth, rate limit, security)       │   │
│  │   WebSocket Hubs (deploy, build, execution)                  │   │
│  └──────────────────────────────┬───────────────────────────────┘   │
│                                 │                                    │
│  ┌──────────────────────────────┴───────────────────────────────┐   │
│  │                      Service Layer                            │   │
│  │   deploy · vm · blueprint · factory · webhook · audit        │   │
│  │   target · template · executor · ldap · bulk                 │   │
│  └──────────────────────────────┬───────────────────────────────┘   │
│                                 │                                    │
│  ┌──────────────────────────────┴───────────────────────────────┐   │
│  │              Provider Registry (plug-in model)                │   │
│  │        vSphere (govmomi)  ·  Proxmox (REST)  ·  ESXi          │   │
│  └──────────────────────────────┬───────────────────────────────┘   │
│                                 │                                    │
│  ┌──────────────────────────────┴───────────────────────────────┐   │
│  │            SQLite (WAL mode, encrypted secrets)               │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │              Template Factory (Packer engine)                 │   │
│  │   OS Definitions · HCL Generation · Build Scheduler          │   │
│  └──────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

### Repository Structure

```
forgemill/
├── cmd/
│   ├── forgemill/           # Server binary entry point (main.go)
│   └── forgemill-cli/       # CLI companion tool (main.go)
├── internal/
│   ├── api/
│   │   ├── router.go        # HTTP router setup, middleware chain
│   │   ├── handlers/        # HTTP handlers (one file per domain)
│   │   ├── middleware/      # Auth, rate limit, security, logging
│   │   └── ws/              # WebSocket hubs (deploy, build, execution)
│   ├── config/              # Environment-based configuration
│   ├── crypto/              # AES-256-GCM encryption utilities
│   ├── db/
│   │   ├── sqlite.go        # Database layer, CRUD operations
│   │   ├── models/          # Data model definitions
│   │   ├── actions.go       # Action CRUD operations
│   │   ├── executions.go    # Execution CRUD operations
│   │   └── migrations/      # Versioned migrations (v1-v32)
│   ├── factory/
│   │   ├── engine.go        # Packer build orchestration
│   │   ├── scheduler.go     # Scheduled rebuilds
│   │   ├── templates.go     # HCL generation
│   │   ├── os_*.go          # OS definitions (Ubuntu, Rocky)
│   │   ├── installer_*.go   # Autoinstall/Kickstart generators
│   │   └── platform_*.go    # vSphere/Proxmox HCL templates
│   ├── provider/
│   │   ├── provider.go      # Provider interface definitions
│   │   ├── registry.go      # Self-registration framework
│   │   ├── metadata.go      # UI metadata (features, fields)
│   │   ├── vmware/          # vSphere + ESXi provider
│   │   └── proxmox/         # Proxmox VE provider
│   ├── service/             # Business logic layer
│   │   ├── deploy.go        # Deployment orchestration
│   │   ├── vm.go            # VM lifecycle management
│   │   ├── factory.go       # Template build management
│   │   ├── webhook.go       # Webhook dispatch
│   │   ├── audit.go         # Audit logging
│   │   └── ...              # Other services
│   └── version/             # Version info (build-time injection)
├── frontend/
│   ├── src/
│   │   ├── api/client.ts    # Axios API client
│   │   ├── hooks/           # useAuth, useWebSocket, etc.
│   │   ├── context/         # React Context providers
│   │   ├── pages/           # Page components
│   │   ├── components/      # Reusable UI components
│   │   └── types/           # TypeScript interfaces
│   ├── package.json
│   └── vite.config.ts
├── docs/                    # Documentation and screenshots
├── secrets/                 # Docker secrets (gitignored)
├── Dockerfile               # Multi-stage build
├── docker-compose.yml       # Development compose
├── docker-compose.prod.yml  # Production compose
├── Makefile                 # Build automation
├── go.mod / go.sum
└── README.md
```

---

## 2. Development Environment Setup

### Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.24+ | Backend development |
| Node.js | 20+ | Frontend development |
| npm | (bundled) | Package management |
| Docker | 24+ | Container builds |
| Docker Compose | v2+ | Local development |
| Packer | latest | Template Factory (optional) |

### Clone and First Run

```bash
# Clone repository
git clone https://github.com/blink-zero/forgemill.git
cd forgemill

# Install dependencies
make deps

# Build everything
make build

# Run (creates secrets automatically on first run)
./bin/forgemill
```

The server will:
1. Auto-generate JWT secret and encryption key if not set
2. Create initial admin user (password printed to stderr)
3. Start on http://localhost:8080

### Environment Variables

Reference `.env.example` for all options:

| Variable | Default | Description |
|----------|---------|-------------|
| `FORGEMILL_LISTEN_ADDR` | `:8080` | HTTP server bind address |
| `FORGEMILL_DB_PATH` | `/app/data/forgemill.db` | SQLite database path |
| `FORGEMILL_DATA_DIR` | `/app/data` | Data directory for builds/artifacts |
| `FORGEMILL_JWT_SECRET` | (auto-generated) | JWT signing key (32+ chars) |
| `FORGEMILL_JWT_SECRET_FILE` | | Docker secrets path |
| `FORGEMILL_ENCRYPTION_KEY` | (auto-generated) | AES encryption key (32+ chars) |
| `FORGEMILL_ENCRYPTION_KEY_FILE` | | Docker secrets path |
| `FORGEMILL_ADMIN_USER` | `admin` | Initial admin username |
| `FORGEMILL_ADMIN_PASSWORD` | (auto-generated) | Initial admin password |
| `FORGEMILL_ADMIN_PASSWORD_FILE` | | Docker secrets path |
| `FORGEMILL_LOG_LEVEL` | `info` | debug, info, warn, error |
| `FORGEMILL_CORS_ORIGINS` | | Comma-separated allowed origins |
| `FORGEMILL_TLS_CERT` | | TLS certificate path |
| `FORGEMILL_TLS_KEY` | | TLS private key path |
| `FORGEMILL_TRUSTED_PROXIES` | | Trusted proxy IPs for X-Forwarded-For |
| `FORGEMILL_ALLOW_PRIVATE_WEBHOOKS` | `false` | Allow webhooks to private IPs |
| `FORGEMILL_JWT_EXPIRY` | `1h` | JWT token lifetime (max 24h enforced) |
| `FORGEMILL_FRONTEND_PATH` | `./frontend/dist` | Static assets path |

### Development Mode

```bash
# Run backend and frontend with hot reload
make dev
```

This starts:
- Go backend on port 8080
- Vite dev server on port 5173 (proxies API to 8080)

### Running Backend/Frontend Separately

```bash
# Terminal 1: Backend
make dev-backend

# Terminal 2: Frontend
make dev-frontend
```

### Docker Development

```bash
# Build and run with Docker Compose
make docker-up

# View logs
docker logs -f forgemill

# Stop
make docker-down
```

### Building for Production

```bash
# Build optimized binaries
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/forgemill ./cmd/forgemill

# Build Docker image with version info
docker build \
  --build-arg VERSION=$(git describe --tags) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t forgemill .
```

---

## 3. Architecture Deep Dive

### Package Structure & Responsibilities

#### API Layer (`internal/api/`)

| File/Package | Responsibility |
|--------------|----------------|
| `router.go` | HTTP router setup, middleware chain, CORS, route registration |
| `handlers/` | Request parsing, validation, response formatting |
| `middleware/auth.go` | JWT/API key validation, user context injection |
| `middleware/ratelimit.go` | Per-IP rate limiting (token bucket) |
| `middleware/security.go` | Security headers (CSP, HSTS, X-Frame-Options) |
| `middleware/logging.go` | Request/response logging |
| `middleware/bodylimit.go` | Request body size limits (1MB) |
| `ws/hub.go` | Deployment progress WebSocket |
| `ws/execution_hub.go` | Action execution output streaming |

#### Service Layer (`internal/service/`)

Services encapsulate business logic and coordinate between database, providers, and external systems.

| Service | Responsibility |
|---------|----------------|
| `deploy.go` | VM deployment orchestration, credential generation, progress tracking |
| `vm.go` | VM CRUD, power ops, snapshots, resize, state sync |
| `factory.go` | Template build lifecycle, family versioning, ISO update checking |
| `target.go` | Target CRUD, template sync, provider instantiation |
| `webhook.go` | Event dispatch, HMAC signing, SSRF protection |
| `audit.go` | Async audit logging with retention cleanup |
| `executor.go` | SSH script execution with parameter injection |
| `blueprint.go` | Blueprint CRUD and deployment |
| `bulk.go` | Batch deployment orchestration |
| `ldap.go` | LDAP authentication and role mapping |
| `cloudinit.go` | Cloud-init YAML merging for actions |

#### Provider Layer (`internal/provider/`)

Providers implement hypervisor-specific operations through a common interface.

```go
type Provider interface {
    Connect(ctx context.Context) error
    Disconnect() error
    TestConnection(ctx context.Context) error
    ListTemplates(ctx context.Context) ([]Template, error)
    DeployVM(ctx context.Context, spec *DeploySpec) (*DeployResult, error)
    GetDeployProgress(ctx context.Context, taskID string) (*Progress, error)
    PowerOn/PowerOff/Restart/Suspend(ctx, vmID) error
    DeleteVM(ctx context.Context, vmID string) error
    // ... snapshots, resize, console, etc.
}
```

| Package | Target Types | SDK |
|---------|--------------|-----|
| `vmware/` | vcenter, esxi | govmomi |
| `proxmox/` | proxmox | REST API |

Providers self-register via `init()`:
```go
func init() {
    registry.RegisterProvider("vcenter", New)
    registry.RegisterProvider("esxi", New)
    registry.RegisterMetadata("vcenter", vCenterMetadata)
}
```

### Request Lifecycle

```
HTTP Request
    │
    ▼
┌───────────────────────────────────────┐
│ 1. SecurityHeaders (CSP, HSTS, etc.)  │
│ 2. RequestID                          │
│ 3. RealIP (if TrustedProxies set)     │
│ 4. Logging (method, path, duration)   │
│ 5. Recoverer (panic recovery)         │
│ 6. MaxBodySize (1MB limit)            │
│ 7. CORS                               │
│ 8. GlobalRateLimiter (60/min/IP)      │
└───────────────────┬───────────────────┘
                    │
                    ▼ (protected routes)
┌───────────────────────────────────────┐
│ 9. Authenticate                       │
│    - Extract Bearer token             │
│    - Validate JWT or API key          │
│    - Check user IsActive              │
│    - Verify TokenVersion              │
│    - Set user in request context      │
└───────────────────┬───────────────────┘
                    │
                    ▼ (role-restricted routes)
┌───────────────────────────────────────┐
│ 10. RequireRole(minRole)              │
│     - Check user.Role >= minRole      │
└───────────────────┬───────────────────┘
                    │
                    ▼
┌───────────────────────────────────────┐
│ Handler                               │
│  1. Parse request body (JSON)         │
│  2. Validate input                    │
│  3. Call service method               │
│  4. Format response (JSON)            │
└───────────────────────────────────────┘
```

### Authentication Flow

**JWT Authentication:**
1. Client sends `Authorization: Bearer <jwt>`
2. Middleware validates signature (HMAC-SHA256), issuer, audience
3. Checks `TokenVersion` matches user's current version (revocation support)
4. Checks `IsActive` flag
5. Sets user in request context

**API Key Authentication:**
1. Client sends `Authorization: Bearer fm_<key>`
2. Middleware extracts prefix (`fm_` + 8 chars)
3. Looks up API key by prefix
4. Compares full key via bcrypt
5. Checks expiration
6. Updates last-used timestamp

**Token Revocation:**
- `POST /api/auth/logout` increments user's `TokenVersion`
- All existing JWTs become invalid (they contain old version)

**Role Hierarchy:**
```
viewer (read-only) < user (deploy capability) < admin (full control)
```

### Database Layer

SQLite with WAL mode. Key pragmas:
- `journal_mode=WAL` - concurrent reads during writes
- `busy_timeout=5000` - 5s wait for locks
- `foreign_keys=ON` - referential integrity

See [Section 5](#5-database--migrations) for schema details.

### WebSocket Architecture

**Three WebSocket Hubs:**

1. **Deploy Hub** (`/api/ws/deploy/:id`)
   - Streams deployment progress to clients
   - Token via WebSocket subprotocol: `Sec-WebSocket-Protocol: token.<jwt>`
   - Max 50 connections per deployment
   - Ping/pong heartbeat every 30s

2. **Build Hub** (`/api/ws/build/:id`)
   - Streams Packer build logs
   - Admin-only access (contains sensitive infrastructure details)
   - Same auth pattern as Deploy Hub

3. **Execution Hub** (`/api/ws/execution/:id`)
   - Streams action execution output
   - Buffers up to 5000 messages for late-connecting clients
   - Viewers can watch (read-only)

### Encryption System

**Algorithm:** AES-256-GCM with HKDF-SHA256 key derivation

```go
// Key derivation (crypto/crypto.go)
reader := hkdf.New(
    sha256.New,
    []byte(key),                      // Input key material (32+ chars)
    []byte("forgemill-enc-v1"),       // Fixed salt
    []byte("forgemill-aes-256-gcm"),  // Info parameter
)
derivedKey := make([]byte, 32)
io.ReadFull(reader, derivedKey)
```

**What Gets Encrypted:**
- Target passwords (`targets.password_encrypted`)
- Deployment initial passwords (`deployments.initial_password_enc`)
- Webhook secrets (`webhooks.secret`)
- LDAP bind passwords (`auth_sources.config_json`)

### Provider System

Providers use a registry pattern for zero-touch registration:

```go
// internal/provider/vmware/client.go
func init() {
    registry.RegisterProvider("vcenter", New)
    registry.RegisterProvider("esxi", New)
    registry.RegisterMetadata("vcenter", &ProviderMetadata{...})
}
```

**Adding a New Provider:**
1. Create `internal/provider/newprovider/client.go`
2. Implement `Provider` interface
3. Register via `init()` function
4. Create `internal/factory/platform_newprovider.go` for Packer HCL
5. Frontend auto-discovers via `/api/targets/types`

### Template Factory Pipeline

```
1. User initiates build (POST /api/factory/builds)
   │
   ▼
2. Engine creates goroutine (2h timeout)
   │
   ▼
3. Generate random SSH password
   │
   ▼
4. Resolve OS definition + platform
   │
   ▼
5. Generate Packer HCL (platform-specific)
   │
   ▼
6. Generate installer config (autoinstall/kickstart)
   │
   ▼
7. Write files with 0600 permissions
   │
   ▼
8. packer init (plugin download)
   │
   ▼
9. packer build -force -machine-readable
   │  (logs streamed via WebSocket)
   │
   ▼
10. On success: sync template, link to build, update family version
```

---

## 4. Coding Standards & Conventions

### Go: Error Handling

**Never ignore errors:**
```go
// WRONG
result, _ := service.DoSomething()

// CORRECT
result, err := service.DoSomething()
if err != nil {
    return fmt.Errorf("do something: %w", err)
}
```

**Wrap errors with context:**
```go
if err := db.CreateUser(user); err != nil {
    return fmt.Errorf("create user %q: %w", user.Username, err)
}
```

**Return early on error:**
```go
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
    var req CreateRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, "invalid request body", http.StatusBadRequest)
        return
    }

    if req.Name == "" {
        writeError(w, "name is required", http.StatusBadRequest)
        return
    }

    // Happy path continues...
}
```

### Go: Handler Structure

Every handler follows this pattern:

```go
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
    // 1. Parse request
    var req CreateRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, "invalid request body", http.StatusBadRequest)
        return
    }

    // 2. Validate input
    if req.Name == "" {
        writeError(w, "name is required", http.StatusBadRequest)
        return
    }

    // 3. Get user from context (if needed)
    user := middleware.UserFromContext(r.Context())

    // 4. Call service
    result, err := h.service.Create(r.Context(), req, user.ID)
    if err != nil {
        // Log full error server-side, return generic message to client
        slog.Error("create failed", "error", err, "user", user.Username)
        writeError(w, "failed to create resource", http.StatusInternalServerError)
        return
    }

    // 5. Audit log (for mutating operations)
    h.audit.Log(user.Username, user.ID, "resource.created", "resource", result.ID, r.RemoteAddr, nil)

    // 6. Return response
    writeJSON(w, http.StatusCreated, result)
}
```

### Go: Service Layer vs Handler vs DB Layer

| Layer | Responsibility | Should NOT do |
|-------|----------------|---------------|
| **Handler** | Parse HTTP, validate input format, call service, format response | Business logic, direct DB calls |
| **Service** | Business logic, orchestration, encryption/decryption, webhooks | HTTP concerns, SQL queries |
| **DB** | SQL queries, transactions, data mapping | Business logic, HTTP concerns |

### Go: Naming Conventions

```go
// Files: lowercase, underscores for word separation
handlers/auth_sources.go
service/bulk.go

// Types: PascalCase
type DeploymentService struct {}
type CreateUserRequest struct {}

// Methods: PascalCase (exported), camelCase (unexported)
func (s *Service) GetUser(id int64) (*User, error)
func (s *Service) validatePassword(p string) error

// Constants: PascalCase for exported, camelCase for unexported
const MaxBodySize = 1 << 20  // 1MB
const defaultTimeout = 30 * time.Second
```

### TypeScript/React: Component Conventions

```tsx
// Functional components with typed props
interface TemplateListProps {
  targetId?: number;
  onSelect: (template: Template) => void;
}

export function TemplateList({ targetId, onSelect }: TemplateListProps) {
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    api.templates.list(targetId)
      .then(res => setTemplates(res.data))
      .catch(err => setError(getErrorMessage(err)))
      .finally(() => setLoading(false));
  }, [targetId]);

  if (loading) return <Skeleton />;
  if (error) return <div className="text-red-500">{error}</div>;

  return (
    <ul>
      {templates.map(t => (
        <li key={t.id} onClick={() => onSelect(t)}>{t.name}</li>
      ))}
    </ul>
  );
}
```

### API Conventions

**REST Patterns:**
```
GET    /api/resources           # List
GET    /api/resources/:id       # Get single
POST   /api/resources           # Create
PUT    /api/resources/:id       # Update
DELETE /api/resources/:id       # Delete
POST   /api/resources/:id/verb  # Action (e.g., /api/vms/:id/power/start)
```

**Response Shapes:**
```json
// Success (single resource)
{ "id": 1, "name": "...", ... }

// Success (list)
[{ "id": 1, ... }, { "id": 2, ... }]

// Success (paginated)
{ "items": [...], "total": 100, "page": 1, "per_page": 20 }

// Error
{ "error": "human-readable message" }
```

**Status Codes:**
- `200` - Success (GET, PUT)
- `201` - Created (POST that creates resource)
- `202` - Accepted (async operations like deploy)
- `204` - No Content (DELETE success)
- `400` - Bad Request (validation error)
- `401` - Unauthorized (missing/invalid auth)
- `403` - Forbidden (insufficient role/ownership)
- `404` - Not Found
- `409` - Conflict (e.g., duplicate name)
- `429` - Rate Limited
- `500` - Internal Server Error

### Logging Patterns

**Use structured logging:**
```go
slog.Info("deployment started",
    "deployment_id", deployment.ID,
    "vm_name", deployment.VMName,
    "user", user.Username,
)

slog.Error("deployment failed",
    "deployment_id", deployment.ID,
    "error", err,
)
```

**Never log:**
- Passwords or credentials
- Full request/response bodies (may contain secrets)
- API keys or tokens

**Do log:**
- Resource IDs and names
- User who performed action
- Operation outcome (success/failure)
- Error messages (server-side only)

### Comments

Only comment:
- Non-obvious business logic
- Security-critical code with security annotation (e.g., `// MED-06: ...`)
- Public interfaces

```go
// requireOwnership checks if the user is the resource creator or an admin.
// Returns 403 Forbidden if ownership check fails.
// HIGH-05: Prevents IDOR attacks on deployment status endpoints.
func requireOwnership(user *User, createdBy int64) bool {
    return user.ID == createdBy || user.Role == "admin"
}
```

---

## 5. Database & Migrations

### How Migrations Work

Migrations are defined in `internal/db/migrations/migrations.go`:

```go
var migrations = []struct {
    version int
    sql     string
}{
    {1, migrationV1},
    {2, migrationV2},
    // ...
    {32, migrationV32},
}
```

**Execution Flow:**
1. Query `schema_version` table for current version
2. Create backup via `VACUUM INTO` if migrations pending
3. Apply each migration in a transaction
4. Execute post-migration hooks (data transforms)

**Backup Location:**
```
/app/data/forgemill.db.backup-v{current_version}
```

### How to Add a New Migration

**Step 1: Create migration SQL**

Add to `internal/db/migrations/migrations.go`:

```go
// Add at the end of the migrations slice
{33, migrationV33},

// Add constant with the SQL
const migrationV33 = `
-- V33: Add new_field to existing_table
ALTER TABLE existing_table ADD COLUMN new_field TEXT DEFAULT '';

-- Or create new table
CREATE TABLE IF NOT EXISTS new_table (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_new_table_name ON new_table(name);
`
```

**Step 2: Handle conditional changes**

For columns that might already exist:
```go
func runMigrationV33Conditionally(db *sql.DB) error {
    // Check if column exists
    var count int
    err := db.QueryRow(`
        SELECT COUNT(*) FROM pragma_table_info('existing_table')
        WHERE name = 'new_field'
    `).Scan(&count)
    if err != nil {
        return err
    }

    if count == 0 {
        _, err = db.Exec(`ALTER TABLE existing_table ADD COLUMN new_field TEXT DEFAULT ''`)
        return err
    }
    return nil
}
```

**Step 3: Add post-migration hook if needed**

For data transformations:
```go
func RunWithBackup(db *sql.DB, dbPath string) error {
    // ... existing code ...

    // After all migrations applied
    if current < 33 && latest >= 33 {
        if err := postMigrationV33(db); err != nil {
            return fmt.Errorf("post-migration v33: %w", err)
        }
    }
}

func postMigrationV33(db *sql.DB) error {
    // Transform existing data
    _, err := db.Exec(`
        UPDATE existing_table
        SET new_field = 'default_value'
        WHERE new_field = '' OR new_field IS NULL
    `)
    return err
}
```

### SQLite Pragmas

Set at connection time:
```go
connStr := path + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON"
```

- `journal_mode=WAL` - Write-Ahead Logging for concurrent reads
- `busy_timeout=5000` - Wait 5s for locks before failing
- `foreign_keys=ON` - Enforce referential integrity

### Key Tables

| Table | Purpose |
|-------|---------|
| `users` | Authentication, roles, token versioning |
| `targets` | Hypervisor connection details |
| `templates` | VM templates with lifecycle status |
| `deployments` | Deployment jobs and history |
| `managed_vms` | Post-deployment VM tracking |
| `actions` | Automation scripts with parameters |
| `action_executions` | Execution history and output |
| `template_builds` | Packer build records |
| `template_families` | Template version grouping |
| `template_schedules` | Automated rebuild schedules |
| `audit_logs` | User action audit trail |

### Query Patterns

**Always use parameterized queries:**
```go
// CORRECT
db.Query("SELECT * FROM users WHERE username = ?", username)

// WRONG - SQL injection risk
db.Query("SELECT * FROM users WHERE username = '" + username + "'")
```

**Escape LIKE patterns:**
```go
func escapeLike(s string) string {
    s = strings.ReplaceAll(s, `\`, `\\`)
    s = strings.ReplaceAll(s, `%`, `\%`)
    s = strings.ReplaceAll(s, `_`, `\_`)
    return s
}

// Usage
db.Query("SELECT * FROM vms WHERE name LIKE ?", "%"+escapeLike(search)+"%")
```

---

## 6. Testing

### Running Tests

```bash
# Run all tests
make test
# or
go test ./...

# Run with verbose output
go test -v ./...

# Run specific package
go test -v ./internal/service/...

# Run with coverage
go test -cover ./...
```

### Test File Locations

Tests live alongside the code they test:
```
internal/service/cloudinit.go
internal/service/cloudinit_test.go
```

### What Must Be Tested

| Category | Examples | Priority |
|----------|----------|----------|
| Crypto | Encryption/decryption round-trip, key derivation | Critical |
| Migrations | Schema changes, data transformations | Critical |
| Input validation | Request parsing, field validation | High |
| Business logic | Deployment state machine, role checks | High |
| Cloud-init merging | YAML merge, action injection | Medium |

### Test Pattern Example

```go
// internal/service/cloudinit_test.go
func TestMergeCloudConfigs(t *testing.T) {
    tests := []struct {
        name     string
        base     string
        actions  []string
        expected string
        wantErr  bool
    }{
        {
            name:    "empty actions",
            base:    "#cloud-config\npackages:\n  - vim\n",
            actions: nil,
            expected: "#cloud-config\npackages:\n  - vim\n",
        },
        {
            name: "merge packages",
            base: "#cloud-config\npackages:\n  - vim\n",
            actions: []string{`{"packages":["curl"]}`},
            expected: "#cloud-config\npackages:\n  - vim\n  - curl\n",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := mergeCloudConfigs(tt.base, tt.actions)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.expected {
                t.Errorf("got:\n%s\nwant:\n%s", got, tt.expected)
            }
        })
    }
}
```

### E2E Test Script

`e2e-test.sh` performs integration testing:
```bash
./e2e-test.sh
```

Covers:
- Login authentication
- Target creation (ESXi, Proxmox)
- Connection testing
- Template sync

---

## 7. Security Standards (Non-Negotiable)

### Encrypted Credentials at Rest

**All credentials MUST be encrypted with AES-256-GCM:**
- Target passwords (`targets.password_encrypted`)
- Deployment initial passwords (`deployments.initial_password_enc`)
- Webhook secrets
- LDAP bind passwords

```go
// Encrypting (service layer)
encryptedPassword, err := s.encryptor.Encrypt(plainPassword)

// Decrypting (service layer)
plainPassword, err := s.encryptor.Decrypt(encryptedPassword)
```

**NEVER store plaintext passwords in the database.**

### Secrets Loading

Production secrets loaded from Docker secrets files:
```go
// Config loading (config/config.go)
value := os.Getenv("FORGEMILL_JWT_SECRET")
if value == "" {
    if filePath := os.Getenv("FORGEMILL_JWT_SECRET_FILE"); filePath != "" {
        content, _ := os.ReadFile(filePath)
        value = strings.TrimSpace(string(content))
    }
}
```

**NEVER use environment variables for secrets in production.**

### JWT Security

- Short-lived tokens (default 1h, max 24h enforced)
- Token versioning for immediate invalidation on logout
- Validates issuer (`forgemill`) and audience (`forgemill`)
- HS256 signature with 32+ byte secret

```go
// Logout increments token version
db.IncrementTokenVersion(userID)
// All existing JWTs now invalid (they contain old version)
```

### Input Validation

**Every handler MUST validate before acting:**

```go
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
    var req CreateRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, "invalid request body", http.StatusBadRequest)
        return
    }

    // Validate EVERY required field
    if req.Name == "" {
        writeError(w, "name is required", http.StatusBadRequest)
        return
    }

    // Validate format/range
    if !validNamePattern.MatchString(req.Name) {
        writeError(w, "invalid name format", http.StatusBadRequest)
        return
    }

    // Service call only after validation
    // ...
}
```

### Rate Limiting

Built-in rate limits:
- Login: 5 requests/minute per IP
- API: 60 requests/minute per IP (burst 10)

### Role Enforcement

**Every mutating endpoint MUST specify role:**

```go
// router.go
r.Route("/api/targets", func(r chi.Router) {
    r.Use(authMw.Authenticate)
    r.Get("/", handlers.ListTargets)           // viewer+

    r.Group(func(r chi.Router) {
        r.Use(authMw.RequireRole("admin"))     // admin only
        r.Post("/", handlers.CreateTarget)
        r.Delete("/{id}", handlers.DeleteTarget)
    })
})
```

### SQL Safety

**Parameterized queries ONLY - no string interpolation:**

```go
// CORRECT
db.Query("SELECT * FROM users WHERE id = ?", userID)

// WRONG - SQL injection vulnerability
db.Query("SELECT * FROM users WHERE id = " + userID)
```

### No Secrets in Logs

```go
// CORRECT - log IDs, not credentials
slog.Info("target created", "id", target.ID, "name", target.Name, "type", target.Type)

// WRONG - never log passwords
slog.Info("target created", "password", target.Password)  // NEVER DO THIS
```

### TLS Configuration

Production deployments MUST use TLS:
```yaml
environment:
  FORGEMILL_TLS_CERT: /path/to/cert.pem
  FORGEMILL_TLS_KEY: /path/to/key.pem
```

### SSRF Protection

Webhook and template source URLs validated:
```go
// Reject private IP ranges
privateCIDRs := []string{
    "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
    "127.0.0.0/8", "169.254.0.0/16", "fc00::/7",
}
```

### AI Agents: Security Rules

**Things AI agents MUST NEVER do:**

1. **Never store plaintext credentials in database**
2. **Never use string interpolation in SQL queries**
3. **Never log passwords, tokens, or API keys**
4. **Never skip authentication middleware**
5. **Never skip role checks on protected endpoints**
6. **Never commit secrets to git**
7. **Never disable TLS validation in production code**
8. **Never use `--no-verify` git hooks**
9. **Never add endpoints without proper role enforcement**
10. **Never trust user input without validation**

---

## 8. Adding New Features

### Adding a New API Endpoint

**Step 1: Database method** (`internal/db/sqlite.go`)
```go
func (db *DB) CreateNewResource(r *NewResource) error {
    _, err := db.conn.Exec(`
        INSERT INTO new_resources (name, created_at)
        VALUES (?, CURRENT_TIMESTAMP)
    `, r.Name)
    return err
}
```

**Step 2: Service method** (`internal/service/newresource.go`)
```go
func (s *NewResourceService) Create(ctx context.Context, req CreateRequest, userID int64) (*NewResource, error) {
    // Business logic, validation, encryption
    resource := &NewResource{Name: req.Name, CreatedBy: userID}
    if err := s.db.CreateNewResource(resource); err != nil {
        return nil, fmt.Errorf("create resource: %w", err)
    }
    return resource, nil
}
```

**Step 3: Handler** (`internal/api/handlers/newresource.go`)
```go
func (h *NewResourceHandler) Create(w http.ResponseWriter, r *http.Request) {
    var req CreateRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, "invalid request body", http.StatusBadRequest)
        return
    }

    if req.Name == "" {
        writeError(w, "name is required", http.StatusBadRequest)
        return
    }

    user := middleware.UserFromContext(r.Context())

    result, err := h.service.Create(r.Context(), req, user.ID)
    if err != nil {
        slog.Error("create failed", "error", err)
        writeError(w, "failed to create", http.StatusInternalServerError)
        return
    }

    h.audit.Log(user.Username, user.ID, "newresource.created", "newresource", result.ID, r.RemoteAddr, nil)
    writeJSON(w, http.StatusCreated, result)
}
```

**Step 4: Router registration** (`internal/api/router.go`)
```go
r.Route("/api/newresources", func(r chi.Router) {
    r.Use(authMw.Authenticate)
    r.Get("/", handlers.NewResource.List)

    r.Group(func(r chi.Router) {
        r.Use(authMw.RequireRole("user"))  // or "admin"
        r.Post("/", handlers.NewResource.Create)
    })
})
```

**Step 5: TypeScript API** (`frontend/src/api/client.ts`)
```typescript
export const newresources = {
  list: () => api.get<NewResource[]>("/newresources"),
  create: (data: CreateNewResource) => api.post<NewResource>("/newresources", data),
};
```

**Step 6: TypeScript types** (`frontend/src/types/index.ts`)
```typescript
export interface NewResource {
  id: number;
  name: string;
  created_at: string;
}
```

### Adding a New Provider

**Step 1: Create provider package**
```
internal/provider/newprovider/
├── client.go      # Provider implementation + init()
```

**Step 2: Implement Provider interface**
```go
package newprovider

func init() {
    registry.RegisterProvider("newprovider", New)
    registry.RegisterMetadata("newprovider", metadata)
}

var metadata = &provider.ProviderMetadata{
    ID:          "newprovider",
    Name:        "New Provider",
    Description: "New hypervisor support",
    Icon:        "server",
    Defaults:    map[string]string{"port": "443"},
    Features: map[string]bool{
        "folders":  false,
        "clusters": false,
    },
}

func New(hostname string, port int, username, password string, validateCerts bool) provider.Provider {
    return &Provider{...}
}

type Provider struct { ... }

func (p *Provider) Connect(ctx context.Context) error { ... }
func (p *Provider) ListTemplates(ctx context.Context) ([]provider.Template, error) { ... }
// ... implement all interface methods
```

**Step 3: Add Factory platform** (`internal/factory/platform_newprovider.go`)

The `Platform` interface requires implementing six methods. The key addition is
`InstallerHints()` which provides cloud-init datasources and services to installer
templates — this eliminates the need for TargetType conditionals in any installer
template.

```go
package factory

func init() {
    RegisterPlatform(&NewProviderPlatform{})
}

type NewProviderPlatform struct{}

func (p *NewProviderPlatform) Types() []string      { return []string{"newprovider"} }
func (p *NewProviderPlatform) InterfaceName() string { return "eth0" }

func (p *NewProviderPlatform) ProvisionerPackages(osFamily string) []string {
    return []string{"newprovider-guest-agent"}
}

func (p *NewProviderPlatform) InstallerHints() InstallerHints {
    return InstallerHints{
        CloudInitDatasources: "[NewProvider, None]",
        CloudInitExtraLines:  nil,
        PlatformServices:     []string{"newprovider-guest-agent"},
    }
}

func (p *NewProviderPlatform) AdjustTemplateData(data *TemplateData, targetType string, osDef *OSDefinition) {
    // Set platform defaults and resolve boot/provisioner commands from OS def
}

func (p *NewProviderPlatform) GenerateHCL(data TemplateData) (string, error) {
    // Generate Packer HCL template
}
```

> **Full guide:** See [docs/ADDING_OS_DEFINITIONS.md](docs/ADDING_OS_DEFINITIONS.md) for
> detailed instructions on adding new hypervisors, OS versions, and OS families.

### Adding a New OS Definition

For a new version of an existing OS family (e.g. Ubuntu 26.04), just add a
`RegisterOSDefinition()` call — no other files need changing.

For a new OS family with a different install method, you also need a new `Installer`
implementation. See [docs/ADDING_OS_DEFINITIONS.md](docs/ADDING_OS_DEFINITIONS.md)
for the full walkthrough with examples.

```go
// Example: new version of existing family (os_ubuntu.go)
RegisterOSDefinition(OSDefinition{
    ID:              "ubuntu-2604",
    Name:            "Ubuntu 26.04 LTS",
    Family:          "ubuntu",
    Version:         "26.04",
    InstallMethod:   "autoinstall",
    BootCommandCD:   ubuntuBootCommandCD,
    BootCommandHTTP: ubuntuBootCommandHTTP,
    ProvisionerCmds: ubuntuProvisionerCmds,
    // ... remaining fields
})
```

### Adding a New Migration

See [Section 5: How to Add a New Migration](#how-to-add-a-new-migration).

---

## 9. Docker & Deployment

### Multi-Stage Dockerfile

```dockerfile
# Stage 1: Frontend build
FROM node:20-alpine AS frontend-build
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci                           # Deterministic install
COPY frontend/ .
RUN npm run build

# Stage 2: Go build
FROM golang:1.24-alpine AS go-build
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /forgemill ./cmd/forgemill

# Stage 3: Final image
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata packer
WORKDIR /app
COPY --from=go-build /forgemill /app/forgemill
COPY --from=frontend-build /app/frontend/dist /app/frontend/dist

# Non-root user
RUN adduser -D -u 1001 forgemill
USER forgemill

HEALTHCHECK --interval=30s --timeout=5s CMD wget --spider http://localhost:8080/ || exit 1
ENTRYPOINT ["/app/forgemill"]
```

### Production Secrets Setup

```bash
mkdir -p secrets

# Generate secrets
openssl rand -hex 32 > secrets/jwt_secret.txt
openssl rand -hex 32 > secrets/encryption_key.txt
echo "your-secure-admin-password" > secrets/admin_password.txt

# Set permissions
chmod 600 secrets/*.txt
```

### docker-compose.prod.yml

```yaml
services:
  forgemill:
    image: ghcr.io/blink-zero/forgemill:latest
    ports:
      - "8080:8080"
    volumes:
      - forgemill-data:/app/data
    environment:
      FORGEMILL_JWT_SECRET_FILE: /run/secrets/jwt_secret
      FORGEMILL_ENCRYPTION_KEY_FILE: /run/secrets/encryption_key
      FORGEMILL_ADMIN_PASSWORD_FILE: /run/secrets/admin_password
    secrets:
      - jwt_secret
      - encryption_key
      - admin_password
    # Security hardening
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    read_only: true
    tmpfs:
      - /tmp

secrets:
  jwt_secret:
    file: ./secrets/jwt_secret.txt
  encryption_key:
    file: ./secrets/encryption_key.txt
  admin_password:
    file: ./secrets/admin_password.txt

volumes:
  forgemill-data:
```

### Health Check

Built-in health check:
```
GET / → 200 OK (serves frontend)
```

Used by Docker:
```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --spider http://localhost:8080/ || exit 1
```

### Updating a Running Instance

```bash
# Pull latest image
docker compose -f docker-compose.prod.yml pull

# Restart (data preserved in volume, migrations auto-apply)
docker compose -f docker-compose.prod.yml up -d

# Check logs for migration output
docker logs forgemill 2>&1 | grep -i migration
```

### Volume and Data Persistence

All persistent data in `/app/data`:
- `forgemill.db` - SQLite database
- `packer-plugins/` - Packer plugin cache
- `builds/` - Temporary build directories
- `.secrets` - Auto-generated secrets (dev only)

---

## 10. Pull Request & Contribution Standards

### Branch Naming

```
feature/add-hyperv-provider
fix/deployment-timeout-handling
refactor/service-layer-cleanup
docs/update-api-reference
```

### Commit Message Format

Use conventional commits:
```
type(scope): description

[optional body]

[optional footer]
```

**Types:**
- `feat` - New feature
- `fix` - Bug fix
- `refactor` - Code change that neither fixes a bug nor adds a feature
- `docs` - Documentation only
- `test` - Adding or updating tests
- `chore` - Maintenance tasks

**Examples:**
```
feat(factory): add Rocky Linux 9 OS definition

fix(deploy): handle timeout when hypervisor unreachable

refactor(service): extract credential encryption to separate package

docs(api): document webhook payload format
```

### What a PR Must Include

1. **Description**: What changed and why
2. **Testing**: How it was tested
3. **Security**: Any security implications
4. **Migration**: If database changes, migration included

```markdown
## Summary
- Added Rocky Linux 9 support to Template Factory
- Includes kickstart configuration and boot commands

## Test Plan
- [ ] Built Rocky 9 template on vSphere
- [ ] Built Rocky 9 template on Proxmox
- [ ] Verified cloud-init works on deployed VM

## Security Considerations
- No new credentials stored
- No new API endpoints

## Migration
- N/A (no database changes)
```

### Pre-Merge Checklist

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] `go vet ./...` has no warnings
- [ ] No hardcoded credentials or secrets
- [ ] No `fmt.Println` debugging statements
- [ ] Migration tested (if applicable)
- [ ] API changes documented
- [ ] Frontend types updated (if API changed)

### Breaking Changes Policy

Breaking changes require:
1. Major version bump
2. Migration path documented
3. Deprecation notice in previous release

---

## 11. AI Agent Guidelines (Critical)

### Orientation Sequence

When starting work on this codebase:

1. **Read this file** (`DEVELOPMENT.md`) completely
2. **Read the README.md** for feature overview
3. **Identify the domain** (API, service, provider, factory, frontend)
4. **Read relevant package** in this order:
   - Interface definitions first (`provider.go`, `service/*.go` interfaces)
   - Existing implementations for patterns
   - Tests for expected behavior

### File Navigation for Features

**Understanding a feature end-to-end:**
```
1. Router (internal/api/router.go)          → Find endpoint
2. Handler (internal/api/handlers/*.go)     → Request/response format
3. Service (internal/service/*.go)          → Business logic
4. Database (internal/db/*.go)              → Data operations
5. Frontend API (frontend/src/api/client.ts) → TypeScript types
6. Frontend Page (frontend/src/pages/*.tsx)  → UI implementation
```

### Pre-Change Checklist

**Before adding ANY new field:**
- [ ] Does it contain credentials? → MUST be encrypted
- [ ] Does it need to persist? → Add migration
- [ ] Does it affect audit? → Add audit log entry
- [ ] Does it affect other users? → Consider role requirements

### Hard Rules

1. **Never skip middleware** - All routes must go through the middleware chain
2. **Never store plaintext credentials** - Use `encryptor.Encrypt()` always
3. **Always run `go build ./...`** before finishing - Catches compile errors
4. **Check existing patterns first** - Don't invent new patterns when existing ones work

### Common Pitfalls

| Pitfall | Correct Approach |
|---------|------------------|
| Adding endpoint without role check | Every mutating endpoint needs `RequireRole()` |
| Returning sensitive data in lists | List queries exclude password fields |
| SQL string interpolation | Use parameterized queries: `WHERE id = ?` |
| Logging passwords | Log IDs and names, never credentials |
| Skipping input validation | Validate ALL fields before service call |
| Creating new patterns | Check how similar features are implemented |
| Adding fields without migration | Every schema change needs a migration |
| Hardcoding configuration | Use environment variables via config |
| Ignoring error returns | Every error must be handled or wrapped |
| Using `fmt.Println` | Use `slog.Info/Error/Debug` |

### Security Review Checklist

Before completing any PR:

- [ ] No plaintext credentials in database
- [ ] All user input validated
- [ ] SQL uses parameterized queries
- [ ] New endpoints have appropriate role checks
- [ ] Ownership checks for user-specific resources
- [ ] No secrets in logs
- [ ] Error messages don't leak internal details

### Testing Changes

```bash
# Always run before committing
go build ./...
go test ./...
go vet ./...

# For frontend changes
cd frontend && npm run build && npx tsc --noEmit
```

### When Unsure

1. Look for similar existing code first
2. Follow the established pattern
3. If no pattern exists, ask before inventing one
4. When in doubt, add more validation rather than less

---

## Quick Reference

### Common Commands

```bash
# Development
make dev                    # Run with hot reload
make build                  # Build binaries
make test                   # Run tests

# Docker
make docker-up              # Start containers
make docker-down            # Stop containers
docker logs -f forgemill    # View logs

# Database
sqlite3 data/forgemill.db   # Open database
.tables                     # List tables
.schema users               # Show schema

# Git
git log --oneline -20       # Recent commits
git diff HEAD~1             # Last commit changes
```

### Key File Locations

| What | Where |
|------|-------|
| Server entry | `cmd/forgemill/main.go` |
| Config | `internal/config/config.go` |
| Router | `internal/api/router.go` |
| Auth middleware | `internal/api/middleware/auth.go` |
| Encryption | `internal/crypto/crypto.go` |
| Migrations | `internal/db/migrations/migrations.go` |
| Provider interface | `internal/provider/provider.go` |
| Factory engine | `internal/factory/engine.go` |
| Frontend API | `frontend/src/api/client.ts` |
| TypeScript types | `frontend/src/types/index.ts` |

### Environment Variables (Essential)

```bash
FORGEMILL_JWT_SECRET_FILE=/run/secrets/jwt_secret
FORGEMILL_ENCRYPTION_KEY_FILE=/run/secrets/encryption_key
FORGEMILL_ADMIN_PASSWORD_FILE=/run/secrets/admin_password
FORGEMILL_LOG_LEVEL=info
FORGEMILL_DB_PATH=/app/data/forgemill.db
```

---

*Last updated: 2026-03-15*
*Document version: 1.0*
