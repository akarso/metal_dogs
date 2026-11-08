package metaldogs

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/mediastore"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// resolveAssetStore wraps the media backend registered by core storagelocal/s3
// during InitAll (before this plugin). Production Init runs before
// BindMediaStorage, so we use MediaStorage() here — the same instance the
// composition root will bind. No product-side HMAC shim.
func resolveAssetStore(app *plugin.App) (ports.AssetStore, error) {
	if app == nil {
		return nil, fmt.Errorf("metaldogs: app required")
	}
	st, ok := app.MediaStorage()
	if !ok {
		return nil, fmt.Errorf("metaldogs: media storage not registered (enable core storagelocal or storages3)")
	}
	signer, ok := media.AsSignedURLStorage(st)
	if !ok {
		return nil, fmt.Errorf("metaldogs: media storage %q does not implement SignedURLStorage", st.Name())
	}
	return mediastore.New(signer)
}
