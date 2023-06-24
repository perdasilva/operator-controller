package variable_sources

import (
	"context"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/variables"
)

var _ deppy.VariableSource = &PackageUniqueness{}

const VariableTypePackageUniqueness = "olm.variable.package-uniqueness"

type PackageUniqueness struct {
}

func NewPackageUniqueness() *PackageUniqueness {
	return &PackageUniqueness{}
}

func (p PackageUniqueness) VariableSourceID() deppy.Identifier {
	return "package-uniqueness"
}

func (p PackageUniqueness) VariableFilterFunc() deppy.VarFilterFn {
	return func(variable deppy.Variable) bool {
		return variable != nil && variable.Kind() == VariableTypeBundle
	}
}

func (p PackageUniqueness) Update(_ context.Context, resolution deppy.MutableResolutionProblem, nextVariable deppy.MutableVariable) error {
	if packageName, ok := nextVariable.GetProperty("olm.package.name"); !ok || packageName == "" {
		return deppy.Fatalf("property 'olm.package.name' undefined for variable %s", nextVariable.Identifier())
	} else {
		uniquenessVariable := variables.NewMutableVariable(
			deppy.Identifierf("package-uniqueness"),
			VariableTypePackageUniqueness,
			map[string]interface{}{},
		)
		if err := uniquenessVariable.AddAtMost(deppy.Identifierf("package-uniqueness/%s", packageName), 1, nextVariable.Identifier()); err != nil {
			return err
		}
		return resolution.ActivateVariable(uniquenessVariable)
	}
}

func (p PackageUniqueness) Finalize(_ context.Context, resolution deppy.MutableResolutionProblem) error {
	return nil
}
