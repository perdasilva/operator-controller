package main

import (
	"bytes"
	"context"
	"fmt"
	catalogdv1 "github.com/operator-framework/operator-controller/catalogd/api/v1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/cache"
	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	"github.com/sirupsen/logrus"
	"io"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"net/http"
	"net/url"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	olmv1SystemNamespace = "olmv1-system"
)

func main() {
	log := logrus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	catalogName := "operatorhubio"
	k8sClient, operatorClient, err := getClients()
	if err != nil {
		log.Fatalf("error building clients: %v", err)
		os.Exit(1)
	}

	var catalog catalogdv1.ClusterCatalog
	err = operatorClient.Get(ctx, client.ObjectKey{Name: catalogName}, &catalog)
	if err != nil {
		log.Fatalf("error getting catalog '%s': %v", catalogName, err)
		os.Exit(1)
	}

	if err := validateCatalog(&catalog); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	catalogUrl, err := url.Parse(catalog.Status.URLs.Base)
	if err != nil {
		log.Fatalf("error parsing catalog url: %v", err)
	}

}

func validateCatalog(catalog *catalogdv1.ClusterCatalog) error {
	if catalog == nil {
		panic("catalog is nil")
	}
	servingCond := meta.FindStatusCondition(catalog.Status.Conditions, catalogdv1.TypeServing)
	if servingCond == nil {
		return fmt.Errorf("catalog not served: not 'Serving' condition found in status")
	}
	if servingCond.Status == metav1.ConditionFalse {
		return fmt.Errorf("catalog not served: %s", servingCond.Message)
	}
	if catalog.Status.URLs == nil {
		return fmt.Errorf("catalog not served: no URLs found in status")
	}
	if catalog.Status.ResolvedSource == nil {
		return fmt.Errorf("catalog not resolved: no resolved source found")
	}
	return nil
}

func getClients() (*kubernetes.Clientset, client.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, nil, err
	}
	scheme := runtime.NewScheme()
	if err := catalogdv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}
	cls, err := kubernetes.NewForConfig(config.GetConfigOrDie())
	if err != nil {
		return nil, nil, err
	}
	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, err
	}
	return cls, cl, nil
}
