package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/solver"
)

// Solution is returned by the Solver when the internal solver executed successfully.
// A successful execution of the solver can still end in an error when no solution can
// be found.
type Solution struct {
	err       deppy.NotSatisfiable
	selection map[deppy.Identifier]deppy.Variable
	problem   deppy.ResolutionProblem
}

func (s *Solution) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Error     deppy.NotSatisfiable                `json:"error"`
		Selection map[deppy.Identifier]deppy.Variable `json:"selection"`
		Problem   deppy.ResolutionProblem             `json:"problem"`
	}{
		Error:     s.err,
		Selection: s.selection,
		Problem:   s.problem,
	})
}

func (s *Solution) String() string {
	str, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Sprintf("error marshaling solution: %s", err)
	}
	return string(str)
}

func (s *Solution) UnmarshalJSON(jsonBytes []byte) error {
	return errors.New("not implemented")
}

// NotSatisfiable returns the resolution error in case the problem is unsat
// on successful resolution, it will return nil
func (s *Solution) NotSatisfiable() deppy.NotSatisfiable {
	return s.err
}

// SelectedVariables returns the variables that were selected by the solver
// as part of the solution
func (s *Solution) SelectedVariables() map[deppy.Identifier]deppy.Variable {
	return s.selection
}

// IsSelected returns true if the variable identified by the identifier was selected
// in the solution by the resolver. It will return false otherwise.
func (s *Solution) IsSelected(identifier deppy.Identifier) bool {
	_, ok := s.selection[identifier]
	return ok
}

// Problem returns the stated problem of the solution.
func (s *Solution) Problem() deppy.ResolutionProblem {
	return s.problem
}

type solutionOptions struct {
	addVariablesToSolution bool
	disableOrderPreference bool
}

func (s *solutionOptions) apply(options ...Option) *solutionOptions {
	for _, applyOption := range options {
		applyOption(s)
	}
	return s
}

func defaultSolutionOptions() *solutionOptions {
	return &solutionOptions{
		addVariablesToSolution: false,
		disableOrderPreference: false,
	}
}

type Option func(solutionOptions *solutionOptions)

// AddAllVariablesToSolution is a Solve option that instructs the solver to include
// all the variables considered to the Solution it produces
func AddAllVariablesToSolution() Option {
	return func(solutionOptions *solutionOptions) {
		solutionOptions.addVariablesToSolution = true
	}
}

func DisableOrderPreference() Option {
	return func(solutionOptions *solutionOptions) {
		solutionOptions.disableOrderPreference = true
	}
}

// DeppyResolver is a simple solver implementation that takes an entity source group and a constraint aggregator
// to produce a Solution (or error if no solution can be found)
type DeppyResolver struct{}

func NewDeppyResolver() *DeppyResolver {
	return &DeppyResolver{}
}

func (d DeppyResolver) Solve(ctx context.Context, problem deppy.ResolutionProblem, options ...Option) (*Solution, error) {
	solutionOpts := defaultSolutionOptions().apply(options...)

	vars, err := problem.GetVariables()
	if err != nil {
		return nil, err
	}

	opts := []solver.Option{
		solver.WithInput(vars),
	}
	if solutionOpts.disableOrderPreference {
		opts = append(opts, solver.DisableOrderPreference())
	}

	satSolver, err := solver.NewSolver(opts...)
	if err != nil {
		return nil, err
	}

	selection, err := satSolver.Solve(ctx)
	if err != nil && !errors.As(err, &deppy.NotSatisfiable{}) {
		return nil, err
	}

	selectionMap := map[deppy.Identifier]deppy.Variable{}
	for _, variable := range selection {
		selectionMap[variable.Identifier()] = variable
	}

	solution := &Solution{selection: selectionMap, err: nil}
	if err != nil {
		unsatError := deppy.NotSatisfiable{}
		errors.As(err, &unsatError)
		solution.err = unsatError
	}

	solution.problem = problem

	return solution, nil
}
