package metaldogs

import (
	"fmt"

	"github.com/akarso/shopanda/plugins/metaldogs/adapters/mediastore"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func resolveAssetStore(st ports.MediaBackend) (ports.AssetStore, error) {
	if st == nil {
		return nil, fmt.Errorf("metaldogs: media storage not registered (enable core storagelocal or storages3)")
	}
	return mediastore.New(st)
}
