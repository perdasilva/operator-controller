package variable_sources

import (
	"context"
	"github.com/perdasilva/replee/pkg/deppy"
)

type VariableSourceBuilder interface {
	WithUpdateFn(updateFunc func(ctx context.Context, problem deppy.MutableResolutionProblem, variable deppy.Variable) error) VariableSourceBuilder
	WithFinalizeFn(finalizeFunc func(ctx context.Context, problem deppy.MutableResolutionProblem) error) VariableSourceBuilder
	WithVariableFilterFn(variableSourceFilterFn deppy.VarFilterFn) VariableSourceBuilder
	Build(ctx context.Context) deppy.VariableSource
}

func NewVariableSourceBuilder(variableSourceID deppy.Identifier) VariableSourceBuilder {
	return &variableSourceBuilder{
		variableSourceID: variableSourceID,
	}
}

var _ deppy.VariableSource = &variableSourceBuilder{}

type variableSourceBuilder struct {
	variableSourceID       deppy.Identifier
	updateFunc             func(ctx context.Context, problem deppy.MutableResolutionProblem, variable deppy.Variable) error
	finalizeFunc           func(ctx context.Context, problem deppy.MutableResolutionProblem) error
	variableSourceFilterFn deppy.VarFilterFn
}

func (v *variableSourceBuilder) WithUpdateFn(updateFunc func(ctx context.Context, problem deppy.MutableResolutionProblem, variable deppy.Variable) error) VariableSourceBuilder {
	v.updateFunc = updateFunc
	return v
}

func (v *variableSourceBuilder) WithFinalizeFn(finalizeFunc func(ctx context.Context, problem deppy.MutableResolutionProblem) error) VariableSourceBuilder {
	v.finalizeFunc = finalizeFunc
	return v
}

func (v *variableSourceBuilder) WithVariableFilterFn(variableSourceFilterFn deppy.VarFilterFn) VariableSourceBuilder {
	v.variableSourceFilterFn = variableSourceFilterFn
	return v
}

func (v *variableSourceBuilder) Build(_ context.Context) deppy.VariableSource {
	return v
}

func (v *variableSourceBuilder) VariableSourceID() deppy.Identifier {
	return v.variableSourceID
}

func (v *variableSourceBuilder) VariableFilterFunc() deppy.VarFilterFn {
	return v.variableSourceFilterFn
}

func (v *variableSourceBuilder) Update(ctx context.Context, resolution deppy.MutableResolutionProblem, nextVariable deppy.Variable) error {
	if v.updateFunc == nil {
		panic("updateFunc is nil")
	}
	return v.updateFunc(ctx, resolution, nextVariable)
}

func (v *variableSourceBuilder) Finalize(ctx context.Context, resolution deppy.MutableResolutionProblem) error {
	if v.finalizeFunc == nil {
		return nil
	}
	return v.finalizeFunc(ctx, resolution)
}
