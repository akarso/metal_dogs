package metaldogs

import (
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const designIDFieldCode = ports.DesignIDFieldCode

// Same 8-4-4-4-12 hex shape as httpapi.looksLikeUUID.
const designIDPattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`

func registerDesignIDField(h pluginsdk.Host) error {
	return h.RegisterField(extapi.FieldDef{
		Code:        designIDFieldCode,
		Label:       "Design ID",
		Type:        extapi.FieldTypeString,
		Scope:       extapi.TargetCartItem,
		StorageMode: extapi.StorageSnapshot,
		Visibility:  extapi.VisibilityPublic,
		Access:      extapi.Access{PluginOnlyWrite: true},
		Validation:  extapi.Validation{Regex: designIDPattern},
	})
}
