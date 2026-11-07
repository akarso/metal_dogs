package metaldogs

import (
	"crypto/rand"
	"fmt"
	"os"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/localfs"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/mediastore"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

const defaultAssetsPath = "var/metaldogs-assets"

// resolveAssetStore prefers platform SignedURLStorage (private S3 via core
// storages3). Otherwise uses a local private root + HMAC shim for dev/test.
func resolveAssetStore(app *plugin.App) (ports.AssetStore, *localfs.AssetStore, error) {
	if app != nil {
		if st, ok := app.MediaStorage(); ok {
			if signer, ok := st.(media.SignedURLStorage); ok {
				wrapped, err := mediastore.New(signer)
				if err != nil {
					return nil, nil, err
				}
				return wrapped, nil, nil
			}
		}
	}
	root := os.Getenv("SHOPANDA_PLUGINS_METALDOGS_ASSETS_PATH")
	if root == "" {
		root = defaultAssetsPath
	}
	secret := []byte(os.Getenv("SHOPANDA_PLUGINS_METALDOGS_ASSET_SIGN_SECRET"))
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, nil, fmt.Errorf("metaldogs: asset sign secret: %w", err)
		}
	}
	local, err := localfs.NewAssetStore(root, secret, "")
	if err != nil {
		return nil, nil, err
	}
	return local, local, nil
}
