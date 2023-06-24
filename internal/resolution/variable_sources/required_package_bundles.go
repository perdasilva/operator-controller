package variable_sources

import (
	"context"
	"github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/predicates"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/sort"
)

var _ deppy.VariableSource = &RequiredPackageBundles{}

const VariableTypeBundle = "olm.variable.bundle"

type RequiredPackageBundles struct {
	catalogSource input.EntitySource
}

func NewRequiredPackageBundles(catalogSource input.EntitySource) *RequiredPackageBundles {
	return &RequiredPackageBundles{catalogSource: catalogSource}
}

func (r RequiredPackageBundles) VariableSourceID() deppy.Identifier {
	return "required-package-bundles"
}

func (r RequiredPackageBundles) VariableFilterFunc() deppy.VarFilterFn {
	return func(v deppy.Variable) bool {
		return v != nil && v.Kind() == VariableTypeRequiredPackage
	}
}

func (r RequiredPackageBundles) Update(ctx context.Context, resolution deppy.MutableResolutionProblem, nextVariable deppy.MutableVariable) error {
	packageName, ok := nextVariable.GetProperty("olm.package.name")
	if !ok {
		return deppy.Fatalf("required package variable %s missing olm.package.name property", nextVariable.Identifier())
	}
	filterPredicates := []input.Predicate{predicates.WithPackageName(packageName.(string))}
	if packageVersion, ok := nextVariable.GetProperty("olm.package.version"); ok && packageVersion != "" {
		semverRange, err := semver.ParseRange(packageVersion.(string))
		if err != nil {
			return deppy.Fatalf("required package variable %s has invalid olm.package.version property: %v", nextVariable.Identifier(), err)
		}
		filterPredicates = append(filterPredicates, predicates.InSemverRange(semverRange))
	}
	if packageChannel, ok := nextVariable.GetProperty("olm.package.channel"); ok && packageChannel != "" {
		filterPredicates = append(filterPredicates, predicates.InChannel(packageChannel.(string)))
	}
	resultSet, err := r.catalogSource.Filter(ctx, input.And(filterPredicates...))
	if err != nil {
		return deppy.Fatalf("failed to filter catalog source: %v", err)
	}
	resultSet = resultSet.Sort(sort.ByChannelAndVersion)
	var depIDs []deppy.Identifier
	for _, dep := range resultSet {
		depIDs = append(depIDs, deppy.Identifierf(string(dep.Identifier())))
	}

	// add dependency constraint to required-package variable
	if err := nextVariable.AddDependency("choose-from", depIDs...); err != nil {
		return err
	}

	// generate the bundle variables for those dependencies
	for _, dep := range resultSet {
		if bundleVariable, err := entity2variable(&dep); err != nil {
			return err
		} else {
			if err := resolution.ActivateVariable(bundleVariable); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r RequiredPackageBundles) Finalize(_ context.Context, _ deppy.MutableResolutionProblem) error {
	return nil
}
