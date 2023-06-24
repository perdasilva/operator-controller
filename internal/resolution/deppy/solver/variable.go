package solver

import (
	"github.com/perdasilva/replee/pkg/deppy"
)

// zeroVariable is returned by VariableOf in error cases.
type zeroVariable struct{}

func (v zeroVariable) IsActivated(constraintID deppy.Identifier) (bool, error) {
	return false, nil
}

func (v zeroVariable) GetConstraint(constraintID deppy.Identifier) (deppy.Constraint, bool) {
	return nil, false
}

func (v zeroVariable) GetConstraintIDs() []deppy.Identifier {
	return nil
}

func (v zeroVariable) Kind() string {
	return "solver.variable.zero"
}

func (v zeroVariable) GetProperty(key string) (interface{}, bool) {
	return nil, false
}

func (v zeroVariable) GetProperties() map[string]interface{} {
	return nil
}

var _ deppy.Variable = zeroVariable{}

func (zeroVariable) Identifier() deppy.Identifier {
	return ""
}

func (zeroVariable) Constraints() []deppy.Constraint {
	return nil
}
