package deppy

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-air/gini/logic"
	"github.com/go-air/gini/z"
	"strings"
)

// NotSatisfiable is an error composed of a minimal set of applied
// constraints that is sufficient to make a solution impossible.
type NotSatisfiable []AppliedConstraint

func (e NotSatisfiable) Error() string {
	const msg = "constraints not satisfiable"
	if len(e) == 0 {
		return msg
	}
	s := make([]string, len(e))
	for i, a := range e {
		s[i] = a.String()
	}
	return fmt.Sprintf("%s: %s", msg, strings.Join(s, ", "))
}

// Identifier values uniquely identify particular Variables within
// the input to a single call to Solve.
type Identifier string

func (id Identifier) String() string {
	return string(id)
}

func (id Identifier) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.String())
}

func (id *Identifier) UnmarshalJSON(jsonBytes []byte) error {
	if id == nil {
		panic("nil identifier")
	}
	var value string
	if err := json.Unmarshal(jsonBytes, &value); err != nil {
		return err
	}
	*id = Identifier(value)
	return nil
}

func Identifierf(format string, args ...interface{}) Identifier {
	return Identifier(fmt.Sprintf(format, args...))
}

// IdentifierFromString returns an Identifier based on a provided
// string.
func IdentifierFromString(s string) Identifier {
	return Identifier(s)
}

// Variable values are the basic unit of problems and solutions
// understood by this package.
type Variable interface {
	// Identifier returns the Identifier that uniquely identifies
	// this Variable among all other Variables in a given
	// problem.
	Identifier() Identifier
	// Constraints returns the set of constraints that apply to
	// this Variable.
	Constraints() []Constraint
	GetConstraint(constraintID Identifier) (Constraint, bool)
	GetConstraintIDs() []Identifier

	Kind() string
	GetProperty(key string) (interface{}, bool)
	GetProperties() map[string]interface{}

	IsActivated(constraintID Identifier) (bool, error)
}

type MutableVariable interface {
	Variable
	SetProperty(key string, value interface{}) error
	GetProperties() map[string]interface{}
	Merge(other Variable) error
	AddMandatory(constraintID Identifier) error
	RemoveMandatory(constraintID Identifier) error
	AddProhibited(constraintID Identifier) error
	RemoveProhibited(constraintID Identifier) error
	AddConflict(constraintID, variableID Identifier) error
	RemoveConflict(constraintID Identifier) error

	// AddConflictsWithAny(constraintID Identifier, variableIDs ...Identifier) error
	// RemoveConflictWithAny(constraintID Identifier, variableIDs ...Identifier) error
	// AddConflictsWithAll(constraintID Identifier, variableIDs ...Identifier) error
	// RemoveConflictsWithAll(constraintID Identifier, variableIDs ...Identifier) error

	AddDependency(constraintID Identifier, variableIDs ...Identifier) error
	RemoveDependency(constraintID Identifier, variableIDs ...Identifier) error

	// AddDependsOnAny(constraintID Identifier, variableIDs ...Identifier) error
	// RemoveDependsOnAny(constraintID Identifier, variableIDs ...Identifier) error
	// AddDependsOnAll(constraintID Identifier, variableIDs ...Identifier) error
	// RemoveDependsOnAll(constraintID Identifier, variableIDs ...Identifier) error

	AddAtMost(constraintID Identifier, n int, variableIDs ...Identifier) error
	RemoveAtMost(constraintID Identifier, variableIDs ...Identifier) error
	SetAtMostN(constraintID Identifier, n int) error
}

// LitMapping performs translation between the input and output types of
// Solve (Constraints, Variables, etc.) and the variables that
// appear in the SAT formula.
type LitMapping interface {
	LitOf(subject Identifier) z.Lit
	LogicCircuit() *logic.C
}

// Constraint implementations limit the circumstances under which a
// particular Variable can appear in a solution.
type Constraint interface {
	ConstraintID() Identifier
	Kind() string
	GetProperty(key string) (interface{}, bool)
	GetProperties() map[string]interface{}
	String(subject Identifier) string
	Apply(lm LitMapping, subject Identifier) z.Lit
	Order() []Identifier
	Anchor() bool
}

type MutableConstraint interface {
	Constraint
	Merge(other Constraint) error
	SetProperty(key string, value interface{}) error
}

// AppliedConstraint values compose a single Constraint with the
// Variable it applies to.
type AppliedConstraint struct {
	Variable   Variable
	Constraint Constraint
}

// String implements fmt.Stringer and returns a human-readable message
// representing the receiver.
func (a AppliedConstraint) String() string {
	return a.Constraint.String(a.Variable.Identifier())
}

type ResolutionOption func()

type ResolutionProblem interface {
	ResolutionProblemID() Identifier
	GetVariables() ([]Variable, error)
	Options() []ResolutionOption
}

type MutableResolutionProblem interface {
	ResolutionProblem
	ActivateVariable(v MutableVariable) error
	DeactivateVariable(variableID Identifier, kind string) error
	GetMutableVariable(identifier Identifier, kind string) (MutableVariable, error)
	GetMutableVariables() ([]MutableVariable, error)
}

type VarFilterFn func(v Variable) bool

type VariableSource interface {
	VariableSourceID() Identifier
	VariableFilterFunc() VarFilterFn
	Update(ctx context.Context, resolution MutableResolutionProblem, nextVariable Variable) error
	Finalize(ctx context.Context, resolution MutableResolutionProblem) error
}

type Solver interface {
	Solve(context.Context) ([]Variable, error)
}
