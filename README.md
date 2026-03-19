# Forgemill

**Infrastructure, forged to order.**

Self-hosted VM deployment and lifecycle management for VMware vCenter, ESXi, and Proxmox VE.

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.7-3178C6?logo=typescript&logoColor=white)](https://www.typescriptlang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](./LICENSE)

![Forgemill demo](docs/demo.gif)

Forgemill is a self-hosted platform for deploying, managing, and automating virtual machines across multiple hypervisors. It provides a unified web interface, REST API, and CLI for everything from one-click VM deployments to fully automated template pipelines powered by Packer.

---

## Quick Start

Three ways to run Forgemill, from simplest to most flexible.

### Option 1: Plain Docker Run (simplest)

```bash
docker run -d \
  --name forgemill \
  -p 8080:8080 \
  -v forgemill-data:/app/data \
  -e FORGEMILL_JWT_SECRET="$(openssl rand -hex 32)" \
  -e FORGEMILL_ENCRYPTION_KEY="$(openssl rand -hex 32)" \
  -e FORGEMILL_ADMIN_PASSWORD="your-admin-password" \
  ghcr.io/blink-zero/forgemill:latest
```

Open `http://localhost:8080` and log in with `admin` / your chosen password.

### Option 2: Pre-built Image with Docker Compose (recommended)

```bash
mkdir forgemill && cd forgemill

# Download the production compose file
curl -fsSL https://raw.githubusercontent.com/blink-zero/forgemill/main/docker-compose.prod.yml \
  -o docker-compose.prod.yml

# Create secrets (optional but recommended for production)
mkdir -p secrets
openssl rand -hex 32 > secrets/jwt_secret.txt
openssl rand -hex 32 > secrets/encryption_key.txt
echo "your-admin-password" > secrets/admin_password.txt

# Start
docker compose -f docker-compose.prod.yml up -d
```

Open `http://localhost:8080` and log in with `admin` / your chosen password.

### Option 3: Build from Source (for developers)

```bash
git clone https://github.com/blink-zero/forgemill.git
cd forgemill

# Create secrets
mkdir -p secrets
openssl rand -hex 32 > secrets/jwt_secret.txt
openssl rand -hex 32 > secrets/encryption_key.txt
echo "your-admin-password" > secrets/admin_password.txt

# Build and start
docker compose up -d --build
```

If no admin password was set, a random one is printed to the container logs:

```bash
docker logs forgemill 2>&1 | grep "Admin password"
```

---

## Updating

Data is preserved across updates. Migrations run automatically on startup.

```bash
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

For plain `docker run`, stop and remove the container, then run with the new image. The named volume (`forgemill-data`) preserves your data.

---

## Configuration

All configuration is via environment variables. For sensitive values, Forgemill supports Docker secrets via `_FILE` variants.

| Variable | Default | Description |
|----------|---------|-------------|
| `FORGEMILL_LISTEN_ADDR` | `:8080` | HTTP server bind address |
| `FORGEMILL_DB_PATH` | `/app/data/forgemill.db` | SQLite database file path |
| `FORGEMILL_DATA_DIR` | `/app/data` | Data directory for builds and artifacts |
| `FORGEMILL_JWT_SECRET` | *(auto-generated)* | JWT signing key (32+ characters recommended) |
| `FORGEMILL_JWT_SECRET_FILE` | -- | Path to file containing JWT secret |
| `FORGEMILL_JWT_EXPIRY` | `1h` | JWT token expiration (e.g., `30m`, `2h`). Max 24h. |
| `FORGEMILL_ENCRYPTION_KEY` | *(auto-generated)* | AES encryption key for secrets at rest |
| `FORGEMILL_ENCRYPTION_KEY_FILE` | -- | Path to file containing encryption key |
| `FORGEMILL_ADMIN_USER` | `admin` | Initial admin account username |
| `FORGEMILL_ADMIN_PASSWORD` | *(random)* | Initial admin account password |
| `FORGEMILL_ADMIN_PASSWORD_FILE` | -- | Path to file containing admin password |
| `FORGEMILL_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `FORGEMILL_CORS_ORIGINS` | *(empty)* | Comma-separated allowed CORS origins |
| `FORGEMILL_TLS_CERT` | -- | Path to TLS certificate for native HTTPS |
| `FORGEMILL_TLS_KEY` | -- | Path to TLS private key |
| `FORGEMILL_TRUSTED_PROXIES` | -- | Comma-separated trusted reverse proxy IPs |
| `FORGEMILL_ALLOW_PRIVATE_WEBHOOKS` | `false` | Allow webhook delivery to private/RFC1918 IPs |
| `FORGEMILL_FRONTEND_PATH` | `./frontend/dist` | Path to built frontend assets |

### Docker Secrets (optional)

For production deployments, you can use Docker secrets instead of environment variables. The `_FILE` variants tell Forgemill to read the value from a file:

```yaml
environment:
  FORGEMILL_JWT_SECRET_FILE: /run/secrets/jwt_secret
  FORGEMILL_ENCRYPTION_KEY_FILE: /run/secrets/encryption_key
  FORGEMILL_ADMIN_PASSWORD_FILE: /run/secrets/admin_password
secrets:
  jwt_secret:
    file: ./secrets/jwt_secret.txt
```

This prevents secrets from appearing in `docker inspect` output or process listings. Environment variables work fine and take precedence if both are set.

---

## Features

### Multi-Hypervisor Support
- VMware vCenter / vSphere via govmomi SDK
- Proxmox VE via REST API with ticket and API token authentication
- VMware ESXi direct host management without vCenter

### VM Deployment
- Template-based cloning with customization specs
- Real-time progress streaming over WebSocket
- Configurable CPU, memory, disk, network, and datastore
- Resource pool and folder placement (vSphere)
- Cloud-init integration: hostname, SSH keys, password, user-data injection
- Platform-aware advanced options with smart defaults per hypervisor

### VM Lifecycle Management
- Power operations: start, stop, restart, suspend
- Snapshot management: create, revert, delete
- Live resource resizing: CPU, memory, disk expansion
- Web console access (noVNC)
- Managed VM inventory with status tracking

### Blueprints and Bulk Deployment
- Save deployment configurations as reusable blueprints
- Deploy multiple VMs in a single operation with name pattern expansion
- Per-VM network configuration overrides

### Post-Deploy Actions
- Reusable automation scripts that run on deployed VMs via SSH
- Built-in actions: Install Docker, Update System Packages, Collect VM Info, Set Timezone, Extend Partition
- Custom action creation with embedded shell scripts
- Real-time execution output streaming via WebSocket
- Merge actions into cloud-init user-data for zero-touch provisioning

### Dashboard
- Stats for targets, templates, VMs, and actions
- Real-time target health monitoring
- Recent activity feed with deployment and execution history

### Template Factory (Packer Integration)
- ISO-to-template pipeline: upload an ISO definition, Forgemill generates Packer HCL and cloud-init autoinstall configs automatically
- Built-in OS definitions (Ubuntu 24.04 LTS, Ubuntu 22.04 LTS) with extensible framework
- Queued builds with real-time log streaming via WebSocket
- Template versioning with automatic supersedence
- Scheduled rebuilds: interval-based or triggered on ISO checksum changes
- Lifecycle management with retention policies and automated cleanup

### Authentication and Access Control
- Local accounts with bcrypt password hashing
- LDAP / Active Directory with group-based role mapping
- API keys with optional expiration (`fm_*` prefix, bcrypt-hashed)
- Role-based access control: three-tier system (viewer / user / admin)
- JWT authentication with token version revocation on logout
- Session management with configurable JWT expiry

### Webhooks
- Event-driven notifications (deployment started, completed, failed)
- HMAC-SHA256 signed payloads for verification
- Private IP filtering with configurable override
- Encrypted secrets at rest (AES-256-GCM)

---

## Screenshots

> Screenshots coming soon.

---

## CLI Tool

The `forgemill-cli` companion binary provides terminal access to all major operations. Config file: `~/.forgemill.yaml`.

### Commands

```bash
# Login (password prompted securely)
forgemill-cli login --url https://forgemill.example.com --user admin

# List targets
forgemill-cli targets list

# List templates (optionally filter by target)
forgemill-cli templates list
forgemill-cli templates list --target-id 1

# Deploy a VM from template
forgemill-cli deploy --template 5 --name web-01 --cpu 4 --memory 8192

# Deploy from blueprint
forgemill-cli deploy --blueprint 2 --name db-01

# Check deployment status
forgemill-cli status 42

# VM operations
forgemill-cli vms list
forgemill-cli vms power 15 start
forgemill-cli vms power 15 stop
forgemill-cli vms power 15 restart
forgemill-cli vms power 15 suspend

# Snapshots
forgemill-cli vms snapshot create 15 --name "pre-upgrade"
forgemill-cli vms snapshot list 15

# Delete VM
forgemill-cli vms delete 15

# JSON output for scripting
forgemill-cli vms list --json
forgemill-cli templates list --json
```

### Global Flags

| Flag | Description |
|------|-------------|
| `--config` | Config file path (default: `~/.forgemill.yaml`) |
| `--url` | Server URL (overrides config) |
| `--api-key` | API key (overrides config) |
| `--json` | Output as formatted JSON |

---

## API Overview

Forgemill exposes a RESTful API at `/api`. All endpoints require authentication via JWT bearer token or API key.

### Authentication
| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/auth/login` | Authenticate and receive JWT |
| `POST` | `/api/auth/logout` | Invalidate current token |
| `GET` | `/api/auth/me` | Current user info |

### Targets (Hypervisor Connections)
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/targets` | List all targets |
| `POST` | `/api/targets` | Create target (admin) |
| `POST` | `/api/targets/:id/test` | Test connection (admin) |
| `POST` | `/api/targets/:id/sync` | Sync templates from target (admin) |
| `GET` | `/api/targets/:id/resources` | List datastores, networks, folders |

### Templates
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/templates` | List all templates |
| `GET` | `/api/templates/:id` | Get template details |
| `GET` | `/api/templates/:id/history` | Template build lineage |

### Deployments
| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/deploy` | Start VM deployment |
| `GET` | `/api/deploy/:id` | Get deployment status |
| `POST` | `/api/deploy/:id/cancel` | Cancel deployment |
| `POST` | `/api/deploy/bulk` | Start bulk deployment |
| `WS` | `/api/ws/deploy/:id` | Live progress stream |

### Managed VMs
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/vms` | List managed VMs |
| `GET` | `/api/vms/:id` | VM details |
| `POST` | `/api/vms/:id/power/:action` | Power operations (admin) |
| `POST` | `/api/vms/:id/snapshots` | Create snapshot (admin) |
| `PUT` | `/api/vms/:id/resize` | Resize CPU/memory (admin) |
| `GET` | `/api/vms/:id/console` | Get console URL (admin) |
| `POST` | `/api/vms/:id/execute` | Run action on VM (admin) |
| `GET` | `/api/vms/:id/executions` | List action executions |
| `GET` | `/api/vms/:id/credentials` | Get VM initial credentials |
| `POST` | `/api/vms/:id/sync` | Sync single VM status |
| `POST` | `/api/vms/sync-all` | Sync all VM statuses |
| `POST` | `/api/vms/:id/reset-host-key` | Reset SSH host key |
| `PUT` | `/api/vms/:id/disks/:key/expand` | Expand specific disk |
| `DELETE` | `/api/vms/:id` | Delete/untrack VM (admin) |
| `DELETE` | `/api/vms/:id/snapshots/:snapId` | Delete snapshot (admin) |
| `POST` | `/api/vms/:id/snapshots/:snapId/revert` | Revert to snapshot (admin) |

### Blueprints
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/blueprints` | List blueprints |
| `POST` | `/api/blueprints` | Create blueprint |
| `POST` | `/api/blueprints/:id/deploy` | Deploy from blueprint |

### Template Factory
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/factory/os-definitions` | List available OS templates |
| `POST` | `/api/factory/builds` | Start a template build (admin) |
| `GET` | `/api/factory/builds/:id` | Get build status |
| `WS` | `/api/ws/build/:id` | Live build log stream |
| `GET` | `/api/factory/schedules` | List build schedules |
| `POST` | `/api/factory/updates/:id/rebuild` | Rebuild on ISO update (admin) |

### Webhooks and API Keys
| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/webhooks` | Create webhook (admin) |
| `POST` | `/api/api-keys` | Generate API key |

### Actions (Post-Deploy Automation)
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/actions` | List all actions |
| `POST` | `/api/actions` | Create custom action (admin) |
| `PUT` | `/api/actions/:id` | Update action (admin) |
| `DELETE` | `/api/actions/:id` | Delete action (admin) |
| `POST` | `/api/vms/:id/execute` | Run action on VM (admin) |
| `GET` | `/api/vms/:id/executions` | List executions for VM |
| `GET` | `/api/executions/:id` | Get execution details |
| `POST` | `/api/executions/:id/cancel` | Cancel running execution |
| `WS` | `/api/ws/execution/:id` | Live execution output stream |

### Dashboard
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/dashboard` | Dashboard statistics and activity |

### Deployment History
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/history` | List deployment history |
| `GET` | `/api/history/:id` | Get deployment details |
| `DELETE` | `/api/deployment-history` | Clear history (admin) |

### Auth Sources (LDAP/AD)
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/auth-sources` | List auth sources (admin) |
| `POST` | `/api/auth-sources` | Create auth source (admin) |
| `PUT` | `/api/auth-sources/:id` | Update auth source (admin) |
| `DELETE` | `/api/auth-sources/:id` | Delete auth source (admin) |
| `POST` | `/api/auth-sources/:id/test` | Test auth source connection (admin) |

### Users
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/users` | List users (admin) |
| `POST` | `/api/users` | Create user (admin) |
| `PUT` | `/api/users/:id/password` | Change password |

### System
| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/version` | Application version info |

### Rate Limits
- Login: 5 requests/minute per IP
- API: 60 requests/minute per IP (1/s average, burst of 10)

---

## Architecture

```
+---------------------------------------------------------+
|                    Frontend (React)                      |
|          TypeScript  .  Vite  .  Tailwind CSS            |
|               Radix UI  .  React Router                  |
+----------------------------+----------------------------+
                             | REST API + WebSocket
+----------------------------+----------------------------+
|                   Backend (Go 1.24)                      |
|  +----------+  +------------+  +---------------------+  |
|  |   chi    |  |  gorilla/  |  |  Template Factory   |  |
|  |  router  |  | websocket  |  |  (Packer engine)    |  |
|  +----+-----+  +-----+------+  +---------+-----------+  |
|       |               |                  |               |
|  +----+---------------+------------------+-----------+   |
|  |                Service Layer                      |   |
|  |   deploy . vm . webhook . blueprint . factory     |   |
|  +-------------------------+-------------------------+   |
|                            |                             |
|  +-------------------------+-------------------------+   |
|  |         Provider Registry (plug-in model)         |   |
|  |      vSphere (govmomi)  .  Proxmox (REST)  .  ESXi  |
|  +---------------------------------------------------+   |
|                            |                             |
|  +-------------------------+-------------------------+   |
|  |      SQLite (WAL mode, encrypted secrets)         |   |
|  +---------------------------------------------------+   |
+---------------------------------------------------------+
```

### Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.24, chi/v5 router, gorilla/websocket |
| Frontend | React 18, TypeScript 5.7, Vite 6, Tailwind CSS |
| Database | SQLite with WAL mode (via modernc.org/sqlite) |
| Auth | JWT (HS256), bcrypt, LDAP/AD, API keys |
| Encryption | AES-256-GCM with HKDF-SHA256 key derivation |
| Hypervisors | govmomi (vSphere), REST API (Proxmox) |
| Template Builds | HashiCorp Packer with HCL generation |
| CLI | spf13/cobra |

---

## Security

Forgemill has been through eight rounds of security auditing with all CRITICAL, HIGH, and MEDIUM findings remediated:

- Encryption at rest: All stored credentials (hypervisor passwords, webhook secrets, LDAP bind passwords) are encrypted with AES-256-GCM using HKDF-SHA256 derived keys
- Parameterized SQL: All database queries use prepared statements; LIKE patterns are escaped to prevent wildcard injection
- JWT with revocation: HS256 tokens with issuer/audience validation and per-user token version for immediate logout invalidation
- RBAC enforcement: Three-tier role system (viewer/user/admin) enforced at every API endpoint and WebSocket connection
- Rate limiting: Token-bucket rate limiting on login (5/min) and API (60/min) per IP
- Security headers: CSP, HSTS, X-Frame-Options, X-Content-Type-Options on all responses
- Webhook safety: HMAC-SHA256 signed payloads, private IP filtering, redirect validation
- LDAP hardening: Unauthenticated bind protection, generic error messages
- Credential redaction: Sensitive values stripped from Packer build logs before storage
- Input validation: URL path escaping for all provider API calls, request body size limits (1MB)
- Non-root container: Docker image runs as unprivileged user (UID 1001), read-only filesystem, no-new-privileges, all capabilities dropped
- Docker secrets: Production credentials can be mounted via Docker secrets instead of environment variables

---

## Project Structure

```
cmd/
  forgemill/              # Server binary
  forgemill-cli/          # CLI companion tool
internal/
  api/                    # HTTP handlers, middleware, WebSocket
  config/                 # Environment-based configuration
  crypto/                 # AES-256-GCM encryption utilities
  db/                     # SQLite database layer + migrations
  factory/                # Packer build engine + scheduler
  provider/               # Hypervisor provider registry (plug-in model)
    registry.go           # Self-registration framework
    proxmox/              # Proxmox VE provider
    vmware/               # vSphere + ESXi provider
  service/                # Business logic layer
frontend/
  src/
    api/                  # Axios API client
    components/           # React UI components (Radix UI)
    hooks/                # Auth, WebSocket hooks
    pages/                # Page components
    types/                # TypeScript interfaces
```

---

## Contributing

See [DEVELOPMENT.md](./DEVELOPMENT.md) for build instructions, project structure, and development workflow.

Contributions are welcome:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Commit your changes (`git commit -m 'Add my feature'`)
4. Push to the branch (`git push origin feature/my-feature`)
5. Open a Pull Request

Ensure your code passes `go build ./...` and `npm run build` (frontend) before submitting.

---

## License

[MIT](./LICENSE) - 2025-2026 Forgemill Contributors
# branch protection test
