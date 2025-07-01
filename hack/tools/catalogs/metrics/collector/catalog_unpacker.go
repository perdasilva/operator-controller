package collector

import (
	"context"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"golang.org/x/sync/semaphore"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CatalogUnpackResult struct {
	CatalogName           string
	CanonicalImageRef     string
	CatalogFS             fs.FS
	NumPackages           int
	NumBundles            int
	CatalogUnpackDuration time.Duration
	Err                   error
}

type CatalogUnpacker struct {
	CatalogPuller  CachedImagePuller
	BundlePuller   CachedImagePuller
	Progressometer *Progressometer
	NumWorkers     int
}

func (c *CatalogUnpacker) Unpack(ctx context.Context, catalogs ...string) <-chan CatalogUnpackResult {
	output := make(chan CatalogUnpackResult)
	go func() {
		defer func() {
			close(output)
		}()
		sem := semaphore.NewWeighted(int64(c.NumWorkers))
		wg := sync.WaitGroup{}
		for _, catalog := range catalogs {
			_ = sem.Acquire(ctx, 1)
			wg.Add(1)
			go func(catalogRef string) {
				defer func() {
					wg.Done()
					sem.Release(1)
				}()
				unpackStart := time.Now()
				if c.Progressometer != nil {
					c.Progressometer.AddNewCatalog(catalogRef)
					c.Progressometer.NotifyUnpacking(catalogRef)
				}
				catalogFS, canonicalRef, err := c.CatalogPuller.Pull(ctx, filepath.Base(strings.ReplaceAll(catalog, ":", "-")), catalogRef)
				if err != nil {
					if c.Progressometer != nil {
						c.Progressometer.NotifyFailed(catalogRef, err)
					}
					output <- CatalogUnpackResult{
						CatalogName: catalogRef,
						Err:         err,
					}
					return
				}
				numPackages := 0
				numBundles := 0
				if err := declcfg.WalkFS(catalogFS, func(path string, cfg *declcfg.DeclarativeConfig, err error) error {
					if err != nil {
						return err
					}
					numPackages += len(cfg.Packages)
					numBundles += len(cfg.Bundles)
					return nil
				}); err != nil {
					if c.Progressometer != nil {
						c.Progressometer.NotifyFailed(catalogRef, err)
					}
					output <- CatalogUnpackResult{
						CatalogName: catalogRef,
						Err:         err,
					}
					return
				}
				if c.Progressometer != nil {
					c.Progressometer.NotifyUnpacked(catalogRef, numBundles)
				}
				output <- CatalogUnpackResult{
					CatalogName:           catalogRef,
					CanonicalImageRef:     canonicalRef.String(),
					CatalogFS:             catalogFS,
					NumPackages:           numPackages,
					NumBundles:            numBundles,
					CatalogUnpackDuration: time.Since(unpackStart),
				}
			}(catalog)
		}
		wg.Wait()
	}()
	return output
}
