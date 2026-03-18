package factory

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

// listRegisteredDefinitions returns all registered OS definitions as a slice.
func listRegisteredDefinitions() []OSDefinition {
	defs := make([]OSDefinition, 0, len(osDefinitionRegistry))
	for _, def := range osDefinitionRegistry {
		defs = append(defs, def)
	}
	return defs
}
