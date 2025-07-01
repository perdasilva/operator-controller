package main

import (
	"context"
	"fmt"
	"github.com/containers/image/v5/types"
	"github.com/go-logr/logr"
	"github.com/operator-framework/operator-controller/hack/tools/catalogs/metrics/collector"
	"github.com/operator-framework/operator-controller/internal/shared/util/image"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	ctrlrtlog "sigs.k8s.io/controller-runtime/pkg/log"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrlrtlog.SetLogger(logr.Logger{})

	// inputs
	catalogs := []string{
		"registry.redhat.io/redhat/redhat-operator-index:v4.19",
		"registry.redhat.io/redhat/redhat-marketplace-index:v4.19",
		"registry.redhat.io/redhat/certified-operator-index:v4.19",
		"registry.redhat.io/redhat/community-operator-index:v4.19",
	}

	progressometer := collector.NewProgressometer()

	// image pullers
	catalogUnpacker := collector.CatalogUnpacker{
		NumWorkers:     5,
		Progressometer: progressometer,
		CatalogPuller: collector.CachedImagePuller{
			Cache: image.CatalogCache("cache/catalogCache"),
			ImagePuller: &image.ContainersImagePuller{
				SourceCtxFunc: func(ctx context.Context) (*types.SystemContext, error) {
					return &types.SystemContext{}, nil
				},
			},
		},
	}
	catalogWalker := collector.CatalogWalker{
		NumWorkers: 5,
	}
	bundleUnpacker := collector.BundleUnpacker{
		NumWorkers: 8,
		BundleImagePuller: collector.CachedImagePuller{
			Cache: image.BundleCache("cache/bundleCache"),
			ImagePuller: &image.ContainersImagePuller{
				SourceCtxFunc: func(ctx context.Context) (*types.SystemContext, error) {
					return &types.SystemContext{}, nil
				},
			},
		},
	}

	catalogMetrics := collector.NewCatalogMetrics()
	reg := prometheus.NewRegistry()
	catalogMetrics.RegisterMetrics(reg)

	fmt.Printf("Listening on :8088\n\n")

	go func() {
		progressometer.Start(ctx)
		defer progressometer.Done()

		unpackedBundles := bundleUnpacker.UnpackBundles(ctx, catalogWalker.ListBundles(ctx, catalogUnpacker.Unpack(ctx, catalogs...)))
		for b := range unpackedBundles {
			if b.Err != nil {
				progressometer.NotifyError(b.CatalogName, b.Err)
			} else {
				catalogMetrics.Update(b)
				progressometer.NotifyBundleMetricsGathered(b.CatalogName)
			}
		}
	}()

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	log.Fatal(http.ListenAndServe(":8088", nil))
}
