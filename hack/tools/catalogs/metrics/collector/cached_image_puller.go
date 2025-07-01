package collector

import (
	"context"
	"github.com/avast/retry-go/v4"
	"github.com/containers/image/v5/docker/reference"
	"github.com/operator-framework/operator-controller/internal/shared/util/image"
	"io/fs"
	"time"
)

type CachedImagePuller struct {
	Cache       image.Cache
	ImagePuller image.Puller
}

func (c *CachedImagePuller) Pull(ctx context.Context, ownerID string, ref string) (fs.FS, reference.Canonical, error) {
	var imageFS fs.FS
	var canonicalRef reference.Canonical
	err := retry.Do(
		func() error {
			var err error
			imageFS, canonicalRef, _, err = c.ImagePuller.Pull(ctx, ownerID, ref, c.Cache)
			return err
		},
		retry.Context(ctx),
		retry.DelayType(retry.BackOffDelay),
		retry.Attempts(5),
		retry.MaxDelay(15*time.Second))
	return imageFS, canonicalRef, err
}
