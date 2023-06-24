package resolution

import (
	"context"
	"fmt"
	"github.com/perdasilva/replee/pkg/deppy"
	s "github.com/perdasilva/replee/pkg/deppy/variable_sources"
	"reflect"
)

var _ deppy.MutableResolutionProblem = &resolutionProblemBuilder{}

type ResolutionProblemBuilder interface {
	WithVariableSources(variableSources ...deppy.VariableSource) ResolutionProblemBuilder
	Build(ctx context.Context) (deppy.ResolutionProblem, error)
}

type resolutionProblemBuilder struct {
	MutableResolutionProblem
	variableSources map[deppy.Identifier]deppy.VariableSource
	variableQueue   []deppy.Variable
}

func (b *resolutionProblemBuilder) ActivateVariable(v deppy.MutableVariable) error {
	oldVar, err := b.MutableResolutionProblem.GetMutableVariable(v.Identifier(), v.Kind())
	if err != nil {
		return err
	}

	if reflect.DeepEqual(oldVar, v) {
		return nil
	}
	b.variableQueue = append(b.variableQueue, v)
	return b.MutableResolutionProblem.ActivateVariable(v)
}

func NewResolutionProblemBuilder(problemID deppy.Identifier) ResolutionProblemBuilder {
	return &resolutionProblemBuilder{
		MutableResolutionProblem: *NewMutableResolutionProblem(problemID),
		variableSources:          map[deppy.Identifier]deppy.VariableSource{},
		variableQueue:            []deppy.Variable{},
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
	b.variableQueue = []deppy.Variable{nil}

	var curVar deppy.Variable
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
