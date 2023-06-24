package variable_sources

import (
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
)

func entity2variable(in *input.Entity) (deppy.MutableVariable, error) {
	bundleEntity := entity.NewBundleEntity(in)
	bundleProperties := map[string]interface{}{}
	if name, err := bundleEntity.PackageName(); err == nil && name != "" {
		bundleProperties["olm.package.name"] = name
	} else {
		return nil, deppy.Fatalf("bundle %s missing olm.package.name property", bundleEntity.Identifier())
	}
	if version, err := bundleEntity.Version(); err == nil {
		bundleProperties["olm.package.version"] = version.String()
	} else {
		return nil, deppy.Fatalf("bundle %s missing olm.package.version property", bundleEntity.Identifier())
	}
	if channel, err := bundleEntity.ChannelProperties(); err == nil {
		bundleProperties["olm.package.channel"] = channel.ChannelName
		bundleProperties["olm.package.replaces"] = channel.Replaces
	}
	if bundlePath, err := bundleEntity.BundlePath(); err == nil {
		bundleProperties["olm.package.bundlePath"] = bundlePath
	} else {
		return nil, deppy.Fatalf("bundle %s missing olm.package.bundlePath property", bundleEntity.Identifier())
	}
	if packageRequired, err := bundleEntity.RequiredPackages(); err == nil {
		bundleProperties["olm.package.required"] = packageRequired
	}
	if gvkProvided, err := bundleEntity.ProvidedGVKs(); err == nil {
		bundleProperties["olm.gvk.provided"] = gvkProvided
	}
	if gvkRequired, err := bundleEntity.RequiredGVKs(); err == nil {
		bundleProperties["olm.gvk.required"] = gvkRequired
	}
	return variables.NewMutableVariable(
		deppy.Identifierf(bundleEntity.Identifier().String()),
		VariableTypeBundle,
		bundleProperties,
	), nil
}
