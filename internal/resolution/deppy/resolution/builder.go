package resolution

import (
	"context"
	"fmt"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	s "github.com/operator-framework/operator-controller/internal/resolution/deppy/variable_sources"
)

var _ deppy.MutableResolutionProblem = &resolutionProblemBuilder{}

type ResolutionProblemBuilder interface {
	WithVariableSources(variableSources ...deppy.VariableSource) ResolutionProblemBuilder
	Build(ctx context.Context) (deppy.ResolutionProblem, error)
}

type resolutionProblemBuilder struct {
	MutableResolutionProblem
	variableSources map[deppy.Identifier]deppy.VariableSource
	variableQueue   []deppy.MutableVariable
}

func (b *resolutionProblemBuilder) ActivateVariable(v deppy.MutableVariable) error {
	if v == nil {
		return nil
	}

	oldVar, err := b.MutableResolutionProblem.GetMutableVariable(v.Identifier(), v.Kind())
	if err != nil {
		return err
	}

	changed, err := oldVar.Merge(v)
	if err != nil {
		return err
	}

	if changed {
		b.variableQueue = append(b.variableQueue, v)
	}
	return nil
}

func NewResolutionProblemBuilder(problemID deppy.Identifier) ResolutionProblemBuilder {
	return &resolutionProblemBuilder{
		MutableResolutionProblem: *NewMutableResolutionProblem(problemID),
		variableSources:          map[deppy.Identifier]deppy.VariableSource{},
		variableQueue:            []deppy.MutableVariable{},
	}
}

func (b *resolutionProblemBuilder) WithVariableSources(variableSources ...deppy.VariableSource) ResolutionProblemBuilder {
	for _, variableSource := range variableSources {
		source := &s.AtMostOnceVariableSource{
			VariableSource: &s.FilterableVariableSource{
				VariableSource: variableSource,
			},
		}
		b.variableSources[variableSource.VariableSourceID()] = source
	}
	return b
}

func (b *resolutionProblemBuilder) Build(ctx context.Context) (deppy.ResolutionProblem, error) {
	// nil variable signals to variable sources that only create variables to start creating
	b.variableQueue = []deppy.MutableVariable{nil}

	var curVar deppy.MutableVariable
	for len(b.variableQueue) > 0 {
		curVar, b.variableQueue = b.variableQueue[0], b.variableQueue[1:]
		for _, source := range b.variableSources {
			err := source.Update(ctx, b, curVar)
			if deppy.IsFatalError(err) {
				return nil, err
			}
			if err != nil {
				fmt.Printf("DEBUG: %v\n", err)
			}
			// todo: this can probably be improved
			if err := b.ActivateVariable(curVar); err != nil {
				return nil, err
			}

			if len(b.variableQueue) == 0 {
				err := source.Finalize(ctx, b)
				if deppy.IsFatalError(err) {
					return nil, err
				}
				if err != nil {
					fmt.Printf("DEBUG: %v\n", err)
				}
			}
		}
	}

	return &b.MutableResolutionProblem, nil
}
