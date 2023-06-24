package variable_sources

import (
	"context"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
)

type FilterableVariableSource struct {
	deppy.VariableSource
}

func (f *FilterableVariableSource) Update(ctx context.Context, problem deppy.MutableResolutionProblem, variable deppy.MutableVariable) error {
	filter := f.VariableFilterFunc()
	if (filter == nil && variable == nil) || (filter != nil && filter(variable)) {
		return f.VariableSource.Update(ctx, problem, variable)
	}
	return nil
}

type AtMostOnceVariableSource struct {
	deppy.VariableSource
	successfullyProcessedVars map[deppy.Identifier]struct{}
}

func (a *AtMostOnceVariableSource) Update(ctx context.Context, problem deppy.MutableResolutionProblem, variable deppy.MutableVariable) error {
	if a.successfullyProcessedVars == nil {
		a.successfullyProcessedVars = map[deppy.Identifier]struct{}{}
	}
	if variable == nil {
		if _, ok := a.successfullyProcessedVars[""]; ok {
			return nil
		}
	} else if _, ok := a.successfullyProcessedVars[variable.Identifier()]; ok {
		return nil
	}

	if err := a.VariableSource.Update(ctx, problem, variable); err != nil {
		return err
	}
	if variable == nil {
		a.successfullyProcessedVars[""] = struct{}{}
		return nil
	} else {
		a.successfullyProcessedVars[variable.Identifier()] = struct{}{}
	}
	return nil
}

func (a *AtMostOnceVariableSource) Finalize(ctx context.Context, problem deppy.MutableResolutionProblem) error {
	a.successfullyProcessedVars = nil
	return a.VariableSource.Finalize(ctx, problem)
}
