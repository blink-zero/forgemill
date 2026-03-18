// Package provider metadata defines the UI configuration for each provider type.
// This enables the frontend to dynamically render forms, icons, and features
// without hardcoding provider-specific logic.
package provider

import (
	"sort"
	"sync"
)

// ProviderMetadata contains all UI-facing configuration for a provider type.
type ProviderMetadata struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Icon        string            `json:"icon"`
	Defaults    ProviderDefaults  `json:"defaults"`
	Hints       map[string]string `json:"hints"`
	Features    ProviderFeatures  `json:"features"`
	DeployFields []DeployField    `json:"deploy_fields"`
}

// ProviderDefaults contains default values for target creation forms.
type ProviderDefaults struct {
	Port                int    `json:"port"`
	Username            string `json:"username"`
	NamePlaceholder     string `json:"name_placeholder"`
	HostnamePlaceholder string `json:"hostname_placeholder"`
}

// ProviderFeatures indicates which capabilities this provider supports.
type ProviderFeatures struct {
	Folders          bool `json:"folders"`
	Clusters         bool `json:"clusters"`
	DiskProvisioning bool `json:"disk_provisioning"`
	LinkedClones     bool `json:"linked_clones"`
}

// DeployField defines a field shown in the deploy form for this provider.
type DeployField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Resource    string `json:"resource"`
	Placeholder string `json:"placeholder,omitempty"`
}

var (
	metadataMu       sync.RWMutex
	metadataRegistry = map[string]*ProviderMetadata{}
)

// RegisterMetadata registers UI metadata for a provider type.
// This is typically called from init() alongside RegisterProvider.
func RegisterMetadata(id string, meta *ProviderMetadata) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	meta.ID = id // Ensure ID matches registration key
	metadataRegistry[id] = meta
}

// GetMetadata returns metadata for a specific provider type.
func GetMetadata(id string) *ProviderMetadata {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	return metadataRegistry[id]
}

// GetAllMetadata returns metadata for all registered providers, sorted by ID.
func GetAllMetadata() []*ProviderMetadata {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	
	result := make([]*ProviderMetadata, 0, len(metadataRegistry))
	for _, meta := range metadataRegistry {
		result = append(result, meta)
	}
	
	// Sort by ID for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	
	return result
}
