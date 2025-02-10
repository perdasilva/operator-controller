package main

import (
	"fmt"
	"github.com/operator-framework/operator-controller/hack/tools/catalogs/gen-manifests/generator"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"io"
	"log"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func main() {
	if err := cmd().Execute(); err != nil {
		log.Fatal(err)
	}
}

func cmd() *cobra.Command {
	var opts *generator.Options
	cmd := &cobra.Command{
		Use:   "installer-rbac [bundle]",
		Short: "installer-rbac generates olmv1 cluster extension service account rbac given an olm registry+v1 bundle",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			logger := logrus.New()
			objs, err := generator.GenerateManifests(cmd.Context(), os.DirFS(args[1]), *opts)
			if err != nil {
				logger.Fatal(fmt.Errorf("failed to generate manifests: %w", err))
				os.Exit(1)
			}
			if err := renderManifests(objs, os.Stdout); err != nil {
				logger.Fatal(fmt.Errorf("error rendering manifests: %w", err))
				os.Exit(1)
			}
		},
	}
	opts = generator.AddFlags(cmd)
	return cmd
}

func renderManifests(objs []client.Object, writer io.Writer) error {
	for _, obj := range objs {
		bytes, err := yaml.Marshal(obj)
		if err != nil {
			return err
		}
		if _, err = writer.Write([]byte("---\n")); err != nil {
			return fmt.Errorf("could not write manifest: %w", err)
		}
		if _, err := writer.Write(bytes); err != nil {
			return fmt.Errorf("could not write manifest: %w", err)
		}
	}
	return nil
}
