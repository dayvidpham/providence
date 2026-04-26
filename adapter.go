package provenance

import (
	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/provenance/pkg/ptypes"
)

// RegistryFromBestiary converts bestiary model data into a provenance ModelRegistry.
// Only Provider, Name (as ModelID), DisplayName, and Family are extracted.
func RegistryFromBestiary(models []bestiary.ModelInfo) ptypes.ModelRegistry {
	entries := make([]ptypes.ModelEntry, len(models))
	for i, m := range models {
		entries[i] = ptypes.ModelEntry{
			Provider:    ptypes.Provider(m.Provider),
			Name:        ptypes.ModelID(m.ID),
			DisplayName: m.DisplayName,
			Family:      string(m.Family),
		}
	}
	return NewRegistry(entries)
}
