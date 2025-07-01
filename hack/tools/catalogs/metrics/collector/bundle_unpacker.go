package collector

import (
	"context"
	"fmt"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type BundleUnpackResult struct {
	Bundle
	Resources            *bundle.RegistryV1
	BundleUnpackDuration time.Duration
	CanonicalImageRef    string
}

type BundleUnpacker struct {
	BundleImagePuller CachedImagePuller
	NumWorkers        int
}

func (c *BundleUnpacker) UnpackBundles(ctx context.Context, catalogBundles <-chan Bundle) <-chan BundleUnpackResult {
	output := make(chan BundleUnpackResult)
	go func() {
		defer func() {
			close(output)
		}()
		wg := sync.WaitGroup{}
		for i := 0; i < c.NumWorkers; i++ {
			wg.Add(1)
			go func() {
				defer func() {
					wg.Done()
				}()
				for {
					select {
					case <-ctx.Done():
						return
					case b, ok := <-catalogBundles:
						if !ok {
							return
						}
						unpackResult := BundleUnpackResult{
							Bundle: b,
						}
						if b.Err != nil {
							output <- unpackResult
							continue
						}
						unpackStart := time.Now()
						bundleFS, canonicalImageRef, err := c.BundleImagePuller.Pull(ctx, fmt.Sprintf("%s-%s", strings.ReplaceAll(filepath.Base(b.CatalogName), ":", "-"), b.Name), b.Image)
						if err != nil {
							unpackResult.Err = fmt.Errorf("error pulling bundle image %q: %w", b.Image, err)
							output <- unpackResult
							continue
						}
						unpackResult.CanonicalImageRef = canonicalImageRef.String()
						unpackResult.BundleUnpackDuration = time.Since(unpackStart)

						rv1, err := source.FromFS(bundleFS).GetBundle()
						if err != nil {
							unpackResult.Err = fmt.Errorf("error parsing bundle %q: %w", b.Bundle, err)
							continue
						}
						unpackResult.Resources = &rv1
						output <- unpackResult
					}
				}
			}()
		}
		wg.Wait()
	}()
	return output
}
