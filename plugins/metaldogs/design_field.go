package metaldogs

import (
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const designIDFieldCode = ports.DesignIDFieldCode

// Same 8-4-4-4-12 hex shape as httpapi.looksLikeUUID.
const designIDPattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`

func registerDesignIDField(app *plugin.App) error {
	return app.Extensions().RegisterField(domainext.FieldDef{
		Code:        designIDFieldCode,
		Label:       "Design ID",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot,
		Visibility:  domainext.VisibilityPublic,
		Access:      domainext.Access{PluginOnlyWrite: true},
		Validation:  domainext.Validation{Regex: designIDPattern},
	})
}
