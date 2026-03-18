// Package provider defines the hypervisor provider interface and registry.
//
// The registry allows new hypervisor types to be added without modifying
// the core service layer. Providers register themselves via init() functions.
package provider

import (
	"sort"
	"sync"
)

// ProviderFactory creates a Provider instance for a target.
// This is the function signature that provider packages implement.
type ProviderFactory func(hostname string, port int, username, password string, validateCerts bool) Provider

var (
	registryMu sync.RWMutex
	registry   = map[string]ProviderFactory{}
)

// RegisterProvider registers a provider factory for a target type.
// This is typically called from init() in each provider package.
//
// Example usage in provider/vmware/client.go:
//
//	func init() {
//	    provider.RegisterProvider("vcenter", func(...) provider.Provider {
//	        return New(...)
//	    })
//	}
func RegisterProvider(targetType string, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[targetType] = factory
}

// GetProviderFactory returns the factory for a target type.
// Returns nil if the type is not registered (caller should handle fallback).
func GetProviderFactory(targetType string) ProviderFactory {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[targetType]
}

// RegisteredTypes returns all registered target types in sorted order.
// Used by the API to expose valid target types dynamically.
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// IsRegistered checks if a target type has a registered provider.
func IsRegistered(targetType string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := registry[targetType]
	return ok
}
