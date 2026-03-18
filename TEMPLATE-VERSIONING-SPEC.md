# Template Versioning Redesign

## Problem

Template versioning is broken in several ways:
1. If user names a template `ubuntu-2404-esxi` (no `-v1`), rebuild produces `ubuntu-2404-esxi-v2` — skips v1
2. If user names it `ubuntu-2404-esxi-v1`, rebuild correctly makes `-v2`, but rebuilding `-v2` can produce `-v2` again if DB version is stale
3. The physical VM name on the hypervisor and the DB version number are loosely coupled — they can drift
4. `BaseName()` string parsing is the source of truth for determining the "family" — fragile
5. Template history chain-walking query is limited to one level in each direction
6. No single "family" identity groups all versions of a template together

## Design Principles

1. **Decouple version number from physical name** — DB is source of truth for version
2. **Always enforce consistent naming** — physical names are always `{base_name}-v{N}`
3. **Introduce template families** — single identity for all versions of a template
4. **Platform-agnostic** — versioning logic stays in service layer, platforms just receive a name string
5. **Backward-compatible** — migration auto-creates families from existing templates

## Schema Changes

### New table: `template_families`

```sql
CREATE TABLE template_families (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    base_name TEXT NOT NULL,
    target_id INTEGER NOT NULL REFERENCES targets(id),
    os_definition_id TEXT NOT NULL DEFAULT '',
    latest_version INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(base_name, target_id)
);
```

### Alter `templates` table

```sql
ALTER TABLE templates ADD COLUMN family_id INTEGER REFERENCES template_families(id);
```

### Alter `template_builds` table (no changes needed — already has `version`)

## Code Changes

### 1. `internal/db/models/models.go`

Add new model:

```go
type TemplateFamily struct {
    ID             int64     `json:"id"`
    BaseName       string    `json:"base_name"`
    TargetID       int64     `json:"target_id"`
    OSDefinitionID string    `json:"os_definition_id"`
    LatestVersion  int       `json:"latest_version"`
    CreatedAt      time.Time `json:"created_at"`
}
```

Add `FamilyID *int64` field to the `Template` model.

### 2. `internal/db/migrations/migrations.go`

Add migration to:
- Create `template_families` table
- Add `family_id` column to `templates`
- Seed migration: for each existing managed template, derive `base_name` via `BaseName()`, find or create a family row, set `family_id`, update `latest_version`

### 3. `internal/db/sqlite.go`

Add new DB methods:

```go
func (db *DB) GetOrCreateFamily(baseName string, targetID int64, osDefID string) (*models.TemplateFamily, error)
func (db *DB) GetFamily(id int64) (*models.TemplateFamily, error)
func (db *DB) UpdateFamilyLatestVersion(familyID int64, version int) error
func (db *DB) GetTemplatesByFamily(familyID int64) ([]models.Template, error)
```

Update existing methods:
- `LinkBuildToTemplate` — also set `family_id` on the template and update `template_families.latest_version`
- `GetTemplateHistory` — rewrite to use `WHERE family_id = (SELECT family_id FROM templates WHERE id = ?) ORDER BY version DESC` instead of chain-walking
- All template SELECT queries — include `family_id` in the column list

### 4. `internal/service/factory.go`

**`StartBuild` (new builds):**
- Strip any `-vN` suffix from user-provided `template_name` to get `base_name`
- Get or create a family for `(base_name, target_id)`
- Set version = `family.LatestVersion + 1`
- Set physical template name = `base_name + "-v" + version`
- Store `family_id` reference on the build

**`RebuildTemplate`:**
- Look up the template's `family_id`
- Get `family.LatestVersion + 1` for `nextVersion`
- Set `cfg.TemplateName = family.BaseName + "-v" + nextVersion`
- Remove the `BaseName()` call entirely from this path

**`onBuildComplete`:**
- After `LinkBuildToTemplate`, also:
  - Set `family_id` on the new template
  - Update `template_families.latest_version`

### 5. `internal/factory/scheduler.go`

- `BaseName()` — keep the function but it should only be used for normalising user input on initial build, NOT as version logic. Add a comment marking it as "input normalisation only"
- Scheduled rebuilds should go through the family path same as manual rebuilds

### 6. `internal/api/handlers/factory.go`

- Add endpoint: `GET /api/factory/families` — list all template families
- Add endpoint: `GET /api/factory/families/:id/history` — get all versions for a family
- Existing build/rebuild endpoints — no API changes needed, just pass through

### 7. Frontend (React/TypeScript)

- Template list: group templates by family, show version badge
- Rebuild button: show "will create v{N+1}" confirmation
- Template detail: show version history from family, not chain-walking
- **Optional/later:** Family management UI (rename base_name, etc.)

## Migration Strategy

The seed migration should:
1. Scan all templates where `managed_by_forgemill = TRUE`
2. For each, compute `base_name = BaseName(template.Name)`
3. Group by `(base_name, target_id)`
4. Create a `template_families` row per group
5. Set `family_id` on all matching templates
6. Set `latest_version` = MAX(version) for the family

Templates that are NOT managed by Forgemill (discovered via sync) get `family_id = NULL` — they don't participate in versioning.

## Testing

- Unit tests for `BaseName()` edge cases (no suffix, `-v1`, `-v99`, `-v0`, names with `v` in them like `devbox-v1`)
- Unit test for `GetOrCreateFamily` (create new, return existing)
- Integration test: build → rebuild → rebuild → verify versions are 1, 2, 3 and names are correct
- Integration test: two templates with same base name on different targets = different families
- Integration test: history query returns all versions ordered correctly

## What NOT to change

- Platform code (`platform_vsphere.go`, `platform_proxmox.go`) — they just receive a name string, no changes needed
- Packer HCL templates — they use `{{.TemplateName}}` which will now always be `{base}-v{N}`
- Provider code (`provider/vmware/`, `provider/proxmox/`) — no changes
- Build validation (`ValidateBuildConfig`) — no changes, the name format still passes validation

## Order of Implementation

1. DB migration + models
2. DB methods (CRUD for families)
3. Service layer changes (StartBuild, RebuildTemplate, onBuildComplete)
4. API endpoints for families
5. Frontend updates
6. Tests
