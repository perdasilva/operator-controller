package solver

import (
	"github.com/go-air/gini/z"
	"github.com/perdasilva/replee/pkg/deppy"
)

// zeroConstraint is returned by ConstraintOf in error cases.
type zeroConstraint struct{}

func (c zeroConstraint) ConstraintID() deppy.Identifier {
	return "zero"
}

func (c zeroConstraint) GetProperties() map[string]interface{} {
	return nil
}

func (c zeroConstraint) Kind() string {
	return "solver.constraint.zero"
}

func (c zeroConstraint) GetProperty(key string) (interface{}, bool) {
	return nil, false
}

var _ deppy.Constraint = zeroConstraint{}

func (zeroConstraint) String(subject deppy.Identifier) string {
	return ""
}

func (zeroConstraint) Apply(lm deppy.LitMapping, subject deppy.Identifier) z.Lit {
	return z.LitNull
}

func (zeroConstraint) Order() []deppy.Identifier {
	return nil
}

func (zeroConstraint) Anchor() bool {
	return false
}
