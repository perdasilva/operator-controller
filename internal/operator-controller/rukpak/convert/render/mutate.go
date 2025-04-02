package render

import (
    apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourceMutator func(client.Object) error
type ResourceMutators []ResourceMutator

func (g *ResourceMutators) Append(generator ...ResourceMutator) {
    *g = append(*g, generator...)
}

type ResourceMutatorFactory func() (ResourceMutators, error)

func (m ResourceMutatorFactory) MakeResourceMutators() (ResourceMutators, error) {
    return m()
}

func CustomResourceDefinitionMutator(name string, mutator func(crd *apiextensionsv1.CustomResourceDefinition) error) ResourceMutator {
    return func(obj client.Object) error {
        crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
        if obj.GetName() != name || !ok {
            return nil
        }
        return mutator(crd)
    }
}
