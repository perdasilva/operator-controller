package resolution

import (
	"context"
	"github.com/google/uuid"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/resolution"
	"github.com/operator-framework/operator-controller/internal/resolution/deppy/resolver"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources"

	"github.com/operator-framework/deppy/pkg/deppy/input"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
)

type OperatorResolver struct {
	entitySource input.EntitySource
	client       client.Client
}

func NewOperatorResolver(client client.Client, entitySource input.EntitySource) *OperatorResolver {
	return &OperatorResolver{
		entitySource: entitySource,
		client:       client,
	}
}

func (o *OperatorResolver) Resolve(ctx context.Context) (*resolver.Solution, error) {
	operatorList := v1alpha1.OperatorList{}
	if err := o.client.List(ctx, &operatorList); err != nil {
		return nil, err
	}
	if len(operatorList.Items) == 0 {
		return &resolver.Solution{}, nil
	}

	problem, err := resolution.NewResolutionProblemBuilder(deppy.Identifierf(uuid.New().String())).
		WithVariableSources(
			variable_sources.NewRequiredPackages(operatorList.Items),
			variable_sources.NewRequiredPackageBundles(o.entitySource),
			variable_sources.NewBundlePackageDependenciesSource(o.entitySource),
			variable_sources.NewBundleGVKDependenciesSource(o.entitySource),
			variable_sources.NewPackageUniqueness(),
			variable_sources.NewGVKUniqueness(),
		).
		Build(ctx)
	if err != nil {
		return nil, err
	}

	olmResolver := resolver.NewDeppyResolver()
	return olmResolver.Solve(ctx, problem)
}
