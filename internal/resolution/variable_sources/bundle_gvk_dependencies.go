package variable_sources

import (
	"context"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/predicates"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/sort"
)

var _ deppy.VariableSource = &BundleGVKDependencies{}

type BundleGVKDependencies struct {
	catalogSource input.EntitySource
}

func NewBundleGVKDependenciesSource(catalogSource input.EntitySource) *BundleGVKDependencies {
	return &BundleGVKDependencies{catalogSource: catalogSource}
}

func (b BundleGVKDependencies) VariableSourceID() deppy.Identifier {
	return "bundle-package-dependencies"
}

func (b BundleGVKDependencies) VariableFilterFunc() deppy.VarFilterFn {
	return func(v deppy.Variable) bool {
		return v != nil && v.Kind() == VariableTypeBundle
	}
}

func (b BundleGVKDependencies) Update(ctx context.Context, resolution deppy.MutableResolutionProblem, nextVariable deppy.MutableVariable) error {
	requiredGVKs, ok := nextVariable.GetProperty("olm.gvk.required")
	if !ok {
		return nil
	}
	for _, requiredGVK := range requiredGVKs.([]entity.GVKRequired) {
		gvk := requiredGVK.AsGVK()
		filterPredicate := []input.Predicate{predicates.ProvidesGVK(&gvk)}
		resultSet, err := b.catalogSource.Filter(ctx, input.And(filterPredicate...))
		if err != nil {
			return err
		}
		resultSet = resultSet.Sort(sort.ByChannelAndVersion)
		var depIDs []deppy.Identifier
		for _, dep := range resultSet {
			depIDs = append(depIDs, deppy.Identifierf(string(dep.Identifier())))
		}

		// add gvk dependencies to variable
		if err := nextVariable.AddDependency(deppy.Identifierf("required-gvk/%s:%s:%s", gvk.Group, gvk.Version, gvk.Kind), depIDs...); err != nil {
			return err
		}

		// create bundle variables for those bundles
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

func (b BundleGVKDependencies) Finalize(ctx context.Context, resolution deppy.MutableResolutionProblem) error {
	return nil
}
