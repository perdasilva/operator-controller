package variable_sources

import (
	"context"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/variables"
	olmentity "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
)

var _ deppy.VariableSource = &PackageUniqueness{}

const VariableTypeGVKUniqueness = "olm.variable.gvk-uniqueness"

type GVKUniqueness struct {
}

func NewGVKUniqueness() *GVKUniqueness {
	return &GVKUniqueness{}
}

func (p GVKUniqueness) VariableSourceID() deppy.Identifier {
	return "gvk-uniqueness"
}

func (p GVKUniqueness) VariableFilterFunc() deppy.VarFilterFn {
	return func(variable deppy.Variable) bool {
		return variable != nil && variable.Kind() == VariableTypeBundle
	}
}

func (p GVKUniqueness) Update(_ context.Context, resolution deppy.MutableResolutionProblem, nextVariable deppy.MutableVariable) error {
	if providedGVKs, ok := nextVariable.GetProperty("olm.gvk.provided"); !ok || providedGVKs == "" {
		return nil
	} else {
		for _, gvk := range providedGVKs.([]olmentity.GVK) {
			uniquenessVariable := variables.NewMutableVariable(
				deppy.Identifierf("gvk-uniqueness"),
				VariableTypePackageUniqueness,
				map[string]interface{}{},
			)
			if err := uniquenessVariable.AddAtMost(deppy.Identifierf("gvk-uniqueness/%s:%s:%s", gvk.Group, gvk.Version, gvk.Kind), 1, nextVariable.Identifier()); err != nil {
				return err
			}
			return resolution.ActivateVariable(uniquenessVariable)
		}
	}
	return nil
}

func (p GVKUniqueness) Finalize(_ context.Context, resolution deppy.MutableResolutionProblem) error {
	return nil
}
