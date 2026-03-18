package factory

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/db/models"
)

// UpdateStore is the interface the update checker uses to query managed templates.
type UpdateStore interface {
	ListManagedTemplates() ([]models.Template, error)
	GetTemplate(id int64) (*models.Template, error)
	GetTemplateBuild(id int64) (*models.TemplateBuild, error)
}

// ISOUpdateChecker checks for newer ISOs for managed templates.
type ISOUpdateChecker struct {
	store  UpdateStore
	client *http.Client
}

// NewISOUpdateChecker creates a new update checker.
func NewISOUpdateChecker(store UpdateStore) *ISOUpdateChecker {
	return &ISOUpdateChecker{
		store: store,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CheckAllForUpdates checks all managed templates for ISO updates.
func (c *ISOUpdateChecker) CheckAllForUpdates() ([]models.UpdateAvailable, error) {
	templates, err := c.store.ListManagedTemplates()
	if err != nil {
		return []models.UpdateAvailable{}, fmt.Errorf("list managed templates: %w", err)
	}

	updates := []models.UpdateAvailable{}
	for _, tmpl := range templates {
		if tmpl.BuildID == nil || tmpl.ISOChecksum == "" {
			continue
		}

		build, err := c.store.GetTemplateBuild(*tmpl.BuildID)
		if err != nil {
			slog.Warn("failed to get build for template", "template_id", tmpl.ID, "build_id", *tmpl.BuildID, "error", err)
			continue
		}

		osDef := GetDefinition(build.OSDefinitionID)
		if osDef == nil {
			continue
		}

		update, hasUpdate, err := c.checkTemplate(tmpl, build, osDef)
		if err != nil {
			slog.Warn("failed to check template for updates", "template", tmpl.Name, "error", err)
			continue
		}

		if hasUpdate {
			updates = append(updates, update)
		}
	}

	return updates, nil
}

// CheckTemplateForUpdate checks a specific template for ISO updates.
func (c *ISOUpdateChecker) CheckTemplateForUpdate(templateID int64) (*models.UpdateAvailable, error) {
	tmpl, err := c.store.GetTemplate(templateID)
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}

	if !tmpl.ManagedByForgemill || tmpl.BuildID == nil {
		return nil, fmt.Errorf("template is not managed by forgemill")
	}

	build, err := c.store.GetTemplateBuild(*tmpl.BuildID)
	if err != nil {
		return nil, fmt.Errorf("get build: %w", err)
	}

	osDef := GetDefinition(build.OSDefinitionID)
	if osDef == nil {
		return nil, fmt.Errorf("unknown OS definition: %s", build.OSDefinitionID)
	}

	update, hasUpdate, err := c.checkTemplate(*tmpl, build, osDef)
	if err != nil {
		return nil, err
	}

	if !hasUpdate {
		return nil, nil
	}

	return &update, nil
}

func (c *ISOUpdateChecker) checkTemplate(tmpl models.Template, build *models.TemplateBuild, osDef *OSDefinition) (models.UpdateAvailable, bool, error) {
	latestChecksum, err := c.fetchLatestChecksum(osDef)
	if err != nil {
		return models.UpdateAvailable{}, false, fmt.Errorf("fetch checksum: %w", err)
	}

	// Compare: the stored iso_checksum may include a "file:" prefix or just be the hash
	currentChecksum := tmpl.ISOChecksum
	currentChecksum = strings.TrimPrefix(currentChecksum, "file:")
	currentChecksum = strings.TrimPrefix(currentChecksum, "sha256:")

	// If after stripping prefixes the value is a URL (not a hex hash), skip comparison.
	// This prevents false positives from comparing URLs to hashes.
	if strings.HasPrefix(currentChecksum, "http://") || strings.HasPrefix(currentChecksum, "https://") {
		// Stored checksum is a URL reference, not an actual hash - cannot compare
		return models.UpdateAvailable{}, false, nil
	}

	if currentChecksum == "" || latestChecksum == "" {
		return models.UpdateAvailable{}, false, nil
	}

	if currentChecksum != latestChecksum {
		return models.UpdateAvailable{
			TemplateID:      tmpl.ID,
			TemplateName:    tmpl.Name,
			OSDefinitionID:  build.OSDefinitionID,
			CurrentChecksum: currentChecksum,
			LatestChecksum:  latestChecksum,
			CurrentVersion:  tmpl.Version,
			ISOURL:          osDef.ISOURLPattern,
		}, true, nil
	}

	return models.UpdateAvailable{}, false, nil
}

func (c *ISOUpdateChecker) fetchLatestChecksum(osDef *OSDefinition) (string, error) {
	if osDef.ISOChecksumURL == "" {
		return "", fmt.Errorf("no checksum URL for OS definition %s", osDef.ID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, osDef.ISOChecksumURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch checksum URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum URL returned status %d", resp.StatusCode)
	}

	// Parse checksum file — supports two formats:
	// 1. GNU style:  "hash *filename" or "hash  filename" (Ubuntu SHA256SUMS)
	// 2. BSD style:  "SHA256 (filename) = hash" (Rocky Linux CHECKSUM)
	// Uses exact filename from the ISO URL (same logic as resolveChecksum in engine.go)
	// to avoid false positives when multiple ISO versions appear in the same file.
	isoFilename := extractISOFilename(osDef)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// BSD/Rocky style: "SHA256 (filename) = hash"
		if strings.Contains(line, " = ") && strings.Contains(line, "(") {
			start := strings.Index(line, "(")
			end := strings.Index(line, ")")
			eqPos := strings.Index(line, " = ")
			if start != -1 && end > start && eqPos > end {
				filename := line[start+1 : end]
				checksum := strings.TrimSpace(line[eqPos+3:])
				if filename == isoFilename {
					return checksum, nil
				}
			}
			continue
		}

		// GNU style: "hash *filename" or "hash  filename"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		checksum := parts[0]
		filename := strings.TrimPrefix(parts[1], "*")

		if filename == isoFilename {
			return checksum, nil
		}
	}

	return "", fmt.Errorf("ISO file not found in checksum file for %s", osDef.ID)
}

// extractISOFilename extracts the exact ISO filename from the OS definition's
// ISO URL pattern. Uses the same approach as resolveChecksum in engine.go
// (path.Base of the URL) to ensure consistent matching.
func extractISOFilename(osDef *OSDefinition) string {
	parts := strings.Split(osDef.ISOURLPattern, "/")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	return ""
}
