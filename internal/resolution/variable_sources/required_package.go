package variable_sources

import (
	"context"
	"github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/variables"
)

const VariableTypeRequiredPackage = "olm.variable.required_package"

var _ deppy.VariableSource = &RequiredPackages{}

type RequiredPackages struct {
	operators []v1alpha1.Operator
}

func NewRequiredPackages(operators []v1alpha1.Operator) *RequiredPackages {
	return &RequiredPackages{operators: operators}
}

func (r RequiredPackages) VariableSourceID() deppy.Identifier {
	return "required-packages"
}

func (r RequiredPackages) VariableFilterFunc() deppy.VarFilterFn {
	return nil
}

func (r RequiredPackages) Update(_ context.Context, resolution deppy.MutableResolutionProblem, _ deppy.MutableVariable) error {
	// for each operator CR create a required package variable
	for _, operator := range r.operators {
		variable := variables.NewMutableVariable(
			deppy.Identifierf("required-package/%s", operator.Spec.PackageName),
			VariableTypeRequiredPackage,
			map[string]interface{}{
				"olm.package.name":    operator.Spec.PackageName,
				"olm.package.version": operator.Spec.Version,
				"olm.package.channel": operator.Spec.Channel,
			},
		)
		if err := variable.AddMandatory("mandatory"); err != nil {
			return err
		}
		if err := resolution.ActivateVariable(variable); err != nil {
			return err
		}
	}
	return nil
}

func (r RequiredPackages) Finalize(_ context.Context, _ deppy.MutableResolutionProblem) error {
	return nil
}
