package collector

import (
	"context"
	"fmt"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"golang.org/x/sync/semaphore"
	"sync"
)

type CatalogWalker struct {
	NumWorkers int
}

type Bundle struct {
	*declcfg.Bundle
	CatalogName string
	Err         error
}

func (c *CatalogWalker) ListBundles(ctx context.Context, catalogs <-chan CatalogUnpackResult) <-chan Bundle {
	output := make(chan Bundle)
	go func() {
		defer close(output)
		wg := sync.WaitGroup{}
		sem := semaphore.NewWeighted(int64(c.NumWorkers))
		for catalog := range catalogs {
			_ = sem.Acquire(ctx, 1)
			wg.Add(1)
			go func(c CatalogUnpackResult) {
				defer func() {
					wg.Done()
					sem.Release(1)
				}()
				if catalog.Err != nil {
					output <- Bundle{Err: fmt.Errorf("error walking catalog %q: %v", catalog.CatalogName, catalog.Err)}
					return
				}
				if catalog.CatalogFS == nil {
					output <- Bundle{
						CatalogName: catalog.CatalogName,
						Err:         fmt.Errorf("error walking catalog %q: fs not found", c.CatalogName),
					}
					return
				}
				if err := declcfg.WalkFS(c.CatalogFS, func(path string, cfg *declcfg.DeclarativeConfig, err error) error {
					if err != nil {
						return err
					}
					for _, b := range cfg.Bundles {
						select {
						case <-ctx.Done():
							return ctx.Err()
						default:
							output <- Bundle{
								Bundle:      &b,
								CatalogName: c.CatalogName,
							}
						}
					}
					return nil
				}); err != nil {
					output <- Bundle{CatalogName: c.CatalogName, Err: err}
				}
			}(catalog)
		}
		wg.Wait()
	}()
	return output
}
