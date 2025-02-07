package main

import (
	"context"
	"fmt"
	"github.com/operator-framework/operator-controller/hack/tools/catalogs/gen-manifests/permissions"
	"github.com/operator-framework/operator-controller/internal/rukpak/convert"
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func main() {
	logger := logrus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bundleFs := os.DirFS("/home/perdasilva/repos/perdasilva/operator-controller/render-test/bundle")
	plain, err := convert.Render(ctx, bundleFs, convert.WithInstallNamespace("operator-system")) //, convert.WithWatchNamespaces([]string{"operator"}))
	if err != nil {
		logger.Fatal(fmt.Errorf("error rendering bundle fs: %w", err))
		os.Exit(1)
	}
	objs := permissions.GenerateInstallerRBAC(plain.Objects)
	if err := renderManifests(objs, os.Stdout); err != nil {
		logger.Fatal(fmt.Errorf("error rendering manifests: %w", err))
		os.Exit(1)
	}
}

func renderManifests(objs []client.Object, writer io.Writer) error {
	for _, obj := range objs {
		bytes, err := yaml.Marshal(obj)
		if err != nil {
			return err
		}
		if _, err := writer.Write(bytes); err != nil {
			return fmt.Errorf("could not write manifest: %w", err)
		}
		if _, err = writer.Write([]byte("---\n")); err != nil {
			return fmt.Errorf("could not write manifest: %w", err)
		}
	}
	return nil
}
