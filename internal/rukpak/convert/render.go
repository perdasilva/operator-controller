package convert

import (
	"context"
	"errors"
	"io/fs"
	corev1 "k8s.io/api/core/v1"
)

type renderOptions struct {
	installNamespace string
	watchNamespaces  []string
	createNamespace  bool
}

func (o *renderOptions) applyRenderOptions(opts ...RenderOption) {
	for _, opt := range opts {
		opt(o)
	}
}

func (o *renderOptions) validate() error {
	if o.installNamespace == "" {
		return errors.New("install namespace is required")
	}
	if len(o.watchNamespaces) == 0 {
		o.watchNamespaces = []string{corev1.NamespaceAll}
	}
	return nil
}

type RenderOption func(*renderOptions)

func WithInstallNamespace(namespace string) RenderOption {
	return func(o *renderOptions) {
		o.installNamespace = namespace
	}
}

func WithWatchNamespaces(namespaces []string) RenderOption {
	return func(o *renderOptions) {
		o.watchNamespaces = namespaces
	}
}

func WithCreateNamespaceToggle() RenderOption {
	return func(o *renderOptions) {
		o.createNamespace = true
	}
}

func Render(ctx context.Context, rv1 fs.FS, opts ...RenderOption) (*Plain, error) {
	renderOpts := renderOptions{}
	renderOpts.applyRenderOptions(opts...)
	if err := renderOpts.validate(); err != nil {
		return nil, err
	}

	reg, err := BundleFSToRegistryV1(ctx, rv1)
	if err != nil {
		return nil, err
	}
	return Convert(reg, renderOpts.installNamespace, renderOpts.watchNamespaces)
}
