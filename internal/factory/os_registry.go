package factory

import "sort"

// osDefinitionRegistry holds all registered OS definitions, keyed by ID.
var osDefinitionRegistry = map[string]OSDefinition{}

// RegisterOSDefinition registers an OS definition by its ID.
func RegisterOSDefinition(def OSDefinition) {
	osDefinitionRegistry[def.ID] = def
}

// getRegisteredDefinition returns an OS definition from the registry by ID, or nil if not found.
func getRegisteredDefinition(id string) *OSDefinition {
	def, ok := osDefinitionRegistry[id]
	if !ok {
		return nil
	}
	return &def
}

// listRegisteredDefinitions returns all registered OS definitions sorted by
// ID. The registry is a map, so without the sort the API (and the Factory
// page) would list them in a different order on every request.
func listRegisteredDefinitions() []OSDefinition {
	defs := make([]OSDefinition, 0, len(osDefinitionRegistry))
	for _, def := range osDefinitionRegistry {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
