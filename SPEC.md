# Forgemill - VM Lifecycle Manager

## Overview
Forgemill is a modern web application for deploying and managing virtual machine templates across VMware vCenter, ESXi, and (later) Proxmox environments. It provides a clean web UI for template management, VM deployment with real-time progress, and lifecycle operations.

Inspired by [Deployaroo](https://github.com/blink-zero/deployaroo) but built from scratch with a modern stack and significantly expanded capabilities.

## Tech Stack
- **Backend:** Go (1.26+)
- **Frontend:** React + TypeScript (Vite)
- **Database:** SQLite (embedded, via modernc.org/sqlite or mattn/go-sqlite3)
- **VMware SDK:** govmomi (pure Go vSphere client)
- **API:** REST (chi or echo router) + WebSocket for live deploy progress
- **Auth:** Local accounts with bcrypt passwords, JWT sessions
- **Deployment:** Single Docker container (multi-stage build)

## Project Structure
```
forgemill/
├── cmd/forgemill/main.go          # Entry point
├── internal/
│   ├── api/                        # HTTP handlers + middleware
│   │   ├── router.go
│   │   ├── middleware/
│   │   ├── handlers/
│   │   │   ├── auth.go
│   │   │   ├── targets.go
│   │   │   ├── templates.go
│   │   │   ├── deploy.go
│   │   │   ├── history.go
│   │   │   └── settings.go
│   │   └── ws/                     # WebSocket hub for deploy progress
│   ├── config/                     # App config (env + file)
│   ├── db/                         # Database layer
│   │   ├── sqlite.go
│   │   ├── migrations/
│   │   └── models/
│   ├── provider/                   # Hypervisor provider interface
│   │   ├── provider.go             # Interface definition
│   │   ├── vmware/                 # VMware (vCenter + ESXi) provider
│   │   │   ├── client.go
│   │   │   ├── templates.go
│   │   │   ├── deploy.go
│   │   │   └── lifecycle.go
│   │   └── proxmox/                # Proxmox provider (Phase 2 - stub for now)
│   │       └── client.go
│   └── service/                    # Business logic layer
│       ├── deploy.go
│       ├── template.go
│       └── target.go
├── frontend/                       # React app
│   ├── src/
│   │   ├── App.tsx
│   │   ├── main.tsx
│   │   ├── api/                    # API client
│   │   ├── components/
│   │   │   ├── Layout/
│   │   │   ├── Dashboard/
│   │   │   ├── Templates/
│   │   │   ├── Deploy/
│   │   │   ├── Targets/
│   │   │   ├── History/
│   │   │   └── Settings/
│   │   ├── hooks/
│   │   ├── pages/
│   │   └── types/
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── Dockerfile                      # Multi-stage: build frontend + Go binary
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
├── .gitignore
├── LICENSE                         # MIT
└── README.md
```

## Data Model

### Targets
Hypervisor connections (vCenter, ESXi, or Proxmox).
```sql
CREATE TABLE targets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('vcenter', 'esxi', 'proxmox')),
    hostname TEXT NOT NULL,
    port INTEGER DEFAULT 443,
    username TEXT NOT NULL,
    password_encrypted TEXT NOT NULL,
    validate_certs BOOLEAN DEFAULT FALSE,
    is_default BOOLEAN DEFAULT FALSE,
    status TEXT DEFAULT 'unknown',
    last_connected_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Templates
VM templates registered from targets.
```sql
CREATE TABLE templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target_id INTEGER NOT NULL REFERENCES targets(id),
    name TEXT NOT NULL,
    moref TEXT,                    -- VMware managed object reference
    os_type TEXT,                  -- linux, windows
    os_name TEXT,                  -- ubuntu-22.04, windows-server-2022
    guest_id TEXT,                 -- VMware guest OS identifier
    cpu INTEGER,
    memory_mb INTEGER,
    disk_gb INTEGER,
    notes TEXT,
    icon TEXT,                     -- Icon identifier for UI
    last_synced_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Deployments
```sql
CREATE TABLE deployments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id INTEGER NOT NULL REFERENCES templates(id),
    target_id INTEGER NOT NULL REFERENCES targets(id),
    vm_name TEXT NOT NULL,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed','cancelled')),
    config_json TEXT,              -- Full deploy config as JSON
    started_at DATETIME,
    completed_at DATETIME,
    error_message TEXT,
    created_by INTEGER REFERENCES users(id),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Deployment Logs
```sql
CREATE TABLE deployment_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    deployment_id INTEGER NOT NULL REFERENCES deployments(id),
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    level TEXT DEFAULT 'info',
    message TEXT NOT NULL
);
```

### Users
```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT,
    role TEXT DEFAULT 'user' CHECK(role IN ('admin', 'user', 'viewer')),
    is_active BOOLEAN DEFAULT TRUE,
    last_login_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## Provider Interface

```go
package provider

type Provider interface {
    // Connection
    Connect() error
    Disconnect() error
    TestConnection() error
    
    // Templates
    ListTemplates() ([]Template, error)
    GetTemplate(id string) (*Template, error)
    
    // Deployment
    DeployVM(spec *DeploySpec) (*DeployResult, error)
    GetDeployProgress(taskID string) (*Progress, error)
    
    // Lifecycle
    PowerOn(vmID string) error
    PowerOff(vmID string) error
    Restart(vmID string) error
    DeleteVM(vmID string) error
    GetVMStatus(vmID string) (*VMStatus, error)
}

type Template struct {
    ID       string
    Name     string
    OSType   string
    GuestID  string
    CPU      int
    MemoryMB int
    DiskGB   int
}

type DeploySpec struct {
    TemplateName string
    VMName       string
    Datacenter   string
    Cluster      string
    Datastore    string
    Folder       string
    Network      string
    CPU          int
    MemoryMB     int
    DiskGB       int
    // Network config
    IPAddress    string
    Netmask      string
    Gateway      string
    DNS          []string
    // OS customisation
    Hostname     string
    DomainName   string
}

type DeployResult struct {
    TaskID string
    VMID   string
}

type Progress struct {
    Percent  int
    State    string
    Message  string
}
```

## API Endpoints

### Auth
- `POST /api/auth/login` - Login (returns JWT)
- `POST /api/auth/logout` - Logout
- `GET /api/auth/me` - Current user

### Targets
- `GET /api/targets` - List all targets
- `POST /api/targets` - Add target
- `GET /api/targets/:id` - Get target details
- `PUT /api/targets/:id` - Update target
- `DELETE /api/targets/:id` - Remove target
- `POST /api/targets/:id/test` - Test connection
- `POST /api/targets/:id/sync` - Sync templates from target
- `GET /api/targets/:id/resources` - Get datastores, networks, folders, clusters

### Templates
- `GET /api/templates` - List all templates
- `GET /api/templates/:id` - Get template details

### Deploy
- `POST /api/deploy` - Start deployment
- `GET /api/deploy/:id` - Get deployment status
- `POST /api/deploy/:id/cancel` - Cancel deployment
- `WS /api/ws/deploy/:id` - WebSocket for live progress

### History
- `GET /api/history` - List deployments (paginated, filterable)
- `GET /api/history/:id` - Deployment detail with logs

### Settings
- `GET /api/settings` - Get app settings
- `PUT /api/settings` - Update settings
- `GET /api/users` - List users (admin)
- `POST /api/users` - Create user (admin)

## Frontend Pages

1. **Dashboard** (`/`) - Stats cards (targets, templates, deployments today), recent deployments table, target health indicators
2. **Templates** (`/templates`) - Grid of template cards with OS icon, name, specs. Filter by target/OS
3. **Deploy** (`/deploy`) - Step wizard: Select template → Configure VM (name, resources, network) → Review → Deploy
4. **Deploy Live** (`/deploy/:id`) - Real-time progress with log stream via WebSocket
5. **Targets** (`/targets`) - Manage hypervisor connections. Test connection button
6. **History** (`/history`) - Paginated table of all deployments with status badges, filterable
7. **Settings** (`/settings`) - User management, app config

## UI Design
- Dark mode by default (with light mode toggle)
- Tailwind CSS for styling
- shadcn/ui component library
- Lucide icons
- Clean, minimal, professional — think Portainer or Proxmox web UI aesthetic

## MVP Scope (Phase 1)
1. Go backend scaffolding with all API routes
2. SQLite database with migrations
3. Local auth (admin user created on first run)
4. VMware vCenter provider (connect, list templates, deploy VM, track progress)
5. React frontend with all pages
6. WebSocket deploy progress
7. Docker deployment
8. README with setup instructions

## Phase 2 (Later)
- ESXi direct support (no vCenter required)
- Proxmox VE provider
- Template auto-download (ISO → Packer → Template pipeline)
- API keys for automation
- Webhook notifications

## Phase 3 (Future)
- VM lifecycle management (power ops, snapshots, resize)
- Bulk deployment
- Deployment blueprints/profiles
- CLI companion tool
- LDAP/AD authentication

## Important Notes
- MIT License
- Use Go best practices (interfaces, error handling, structured logging)
- Frontend should be responsive and work on mobile
- All passwords/secrets encrypted at rest (use Go's crypto/aes)
