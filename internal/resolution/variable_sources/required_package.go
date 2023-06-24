package variable_sources

import (
  "context"
  v1 "github.com/operator-framework/api/pkg/operators/v1"
  "github.com/operator-framework/operator-controller/internal/resolution/deppy"
)

var _ deppy.VariableSource = &RequiredPackages{}

type RequiredPackages struct {
  operators []v1.Operator
}

func NewRequiredPackages(operators []v1.Operator) *RequiredPackages {
  return &RequiredPackages{operators: operators}
}

func (r RequiredPackages) VariableSourceID() deppy.Identifier {
  return "required-packages"
}

func (r RequiredPackages) VariableFilterFunc() deppy.VarFilterFn {
  return nil
}

func (r RequiredPackages) Update(_ context.Context, resolution deppy.MutableResolutionProblem, _ deppy.Variable) error {
  for _, operator := range r.operators {
    packageName := operator.Spec.
  }
}

func (r RequiredPackages) Finalize(_ context.Context, _ deppy.MutableResolutionProblem) error {
  return nil
}
