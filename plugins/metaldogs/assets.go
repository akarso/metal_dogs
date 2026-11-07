package metaldogs

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/mediastore"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// resolveAssetStore wraps the composition-root media backend. Core
// storagelocal/s3 register SignedURLStorage during InitAll (before this
// plugin). Prefer BoundMediaStorage when already bound; otherwise the
// registered MediaStorage instance (same object BindMediaStorage will bind).
// No product-side HMAC shim — local signed GETs are platform
// GET /api/v1/media/local-signed/{token}.
func resolveAssetStore(app *plugin.App) (ports.AssetStore, error) {
	if app == nil {
		return nil, fmt.Errorf("metaldogs: app required")
	}
	var st media.Storage
	if bound, ok := app.BoundMediaStorage(); ok {
		st = bound
	} else if registered, ok := app.MediaStorage(); ok {
		st = registered
	} else {
		return nil, fmt.Errorf("metaldogs: media storage not registered (enable core storagelocal or storages3)")
	}
	signer, ok := media.AsSignedURLStorage(st)
	if !ok {
		return nil, fmt.Errorf("metaldogs: media storage %q does not implement SignedURLStorage", st.Name())
	}
	return mediastore.New(signer)
}
