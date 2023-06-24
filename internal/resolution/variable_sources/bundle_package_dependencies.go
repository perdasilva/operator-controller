package variable_sources

import (
	"context"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/predicates"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/sort"
)

var _ deppy.VariableSource = &BundlePackageDependencies{}

type BundlePackageDependencies struct {
	catalogSource input.EntitySource
}

func NewBundlePackageDependenciesSource(catalogSource input.EntitySource) *BundlePackageDependencies {
	return &BundlePackageDependencies{catalogSource: catalogSource}
}

func (b BundlePackageDependencies) VariableSourceID() deppy.Identifier {
	return "bundle-package-dependencies"
}

func (b BundlePackageDependencies) VariableFilterFunc() deppy.VarFilterFn {
	return func(v deppy.Variable) bool {
		return v != nil && v.Kind() == VariableTypeBundle
	}
}

func (b BundlePackageDependencies) Update(ctx context.Context, resolution deppy.MutableResolutionProblem, nextVariable deppy.MutableVariable) error {
	requiredPackages, ok := nextVariable.GetProperty("olm.package.required")
	if !ok {
		return nil
	}
	for _, requiredPackage := range requiredPackages.([]entity.PackageRequired) {
		filterPredicate := []input.Predicate{
			predicates.WithPackageName(requiredPackage.PackageName),
			predicates.InSemverRange(*requiredPackage.SemverRange),
		}
		resultSet, err := b.catalogSource.Filter(ctx, input.And(filterPredicate...))
		if err != nil {
			return deppy.Fatalf("failed to filter catalog source: %v", err)
		}
		resultSet = resultSet.Sort(sort.ByChannelAndVersion)
		var depIDs []deppy.Identifier
		for _, dep := range resultSet {
			depIDs = append(depIDs, deppy.Identifierf(string(dep.Identifier())))
		}
		if err := nextVariable.AddDependency(deppy.Identifierf("required-package/%s", requiredPackage.PackageName), depIDs...); err != nil {
			return err
		}
		for _, dep := range resultSet {
			bundlerVariable, err := entity2variable(&dep)
			if err != nil {
				return err
			}
			if err := resolution.ActivateVariable(bundlerVariable); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b BundlePackageDependencies) Finalize(ctx context.Context, resolution deppy.MutableResolutionProblem) error {
	return nil
}
