package render_test

import (
    "fmt"
    "github.com/operator-framework/api/pkg/operators/v1alpha1"
    "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
    "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert/render"
    "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
    "github.com/stretchr/testify/require"
    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    rbacv1 "k8s.io/api/rbac/v1"
    apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/utils/ptr"
    "os"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "testing"
)

const maxNameLength = 63

func Test_GenerateBundleResources(t *testing.T) {
    bundlefs := os.DirFS("../testdata/argocd-operator.v0.6.0")
    rv1, err := convert.ParseFS(bundlefs)
    require.NoError(t, err)

    bundleRenderer := render.ChainedResourceGenerator(
        render.BundlePermissionsGenerator,
        render.BundleCRDGenerator,
        render.BundleResourceGenerator,
        render.BundleDeploymentGenerator,
    )

    objs, err := bundleRenderer.GenerateResources(&rv1, render.Options{
        InstallNamespace: "argocd-operator",
        TargetNamespaces: []string{""},
        UniqueNameGenerator: func(base string, o interface{}) (string, error) {
            hashStr, err := util.DeepHashObject(o)
            if err != nil {
                return "", err
            }
            if len(base)+len(hashStr) > maxNameLength {
                base = base[:maxNameLength-len(hashStr)-1]
            }

            return fmt.Sprintf("%s-%s", base, hashStr), nil
        },
    })

    require.NoError(t, err)
    require.Len(t, objs, 1)
}

func Test_BundleDeploymentGenerator_Succeeds(t *testing.T) {
    for _, tc := range []struct {
        name              string
        bundle            *convert.RegistryV1
        opts              render.Options
        expectedResources []client.Object
    }{
        {
            name: "generates deployment resources",
            bundle: &convert.RegistryV1{
                CSV: MakeCSV(
                    WithAnnotations(map[string]string{
                        "csv": "annotation",
                    }),
                    WithStrategyDeploymentSpecs(
                        v1alpha1.StrategyDeploymentSpec{
                            Name: "deployment-one",
                            Label: map[string]string{
                                "bar": "foo",
                            },
                            Spec: appsv1.DeploymentSpec{
                                Template: corev1.PodTemplateSpec{
                                    ObjectMeta: metav1.ObjectMeta{
                                        Annotations: map[string]string{
                                            "pod": "annotation",
                                        },
                                    },
                                },
                            },
                        },
                        v1alpha1.StrategyDeploymentSpec{
                            Name: "deployment-two",
                            Spec: appsv1.DeploymentSpec{},
                        },
                    ),
                ),
            },
            opts: render.Options{
                InstallNamespace: "install-namespace",
                TargetNamespaces: []string{"watch-namespace-one", "watch-namespace-two"},
            },
            expectedResources: []client.Object{
                &appsv1.Deployment{
                    TypeMeta: metav1.TypeMeta{
                        Kind:       "Deployment",
                        APIVersion: appsv1.SchemeGroupVersion.String(),
                    },
                    ObjectMeta: metav1.ObjectMeta{
                        Namespace: "install-namespace",
                        Name:      "deployment-one",
                        Labels: map[string]string{
                            "bar": "foo",
                        },
                    },
                    Spec: appsv1.DeploymentSpec{
                        RevisionHistoryLimit: ptr.To(int32(1)),
                        Template: corev1.PodTemplateSpec{
                            ObjectMeta: metav1.ObjectMeta{
                                Annotations: map[string]string{
                                    "csv":                  "annotation",
                                    "olm.targetNamespaces": "watch-namespace-one,watch-namespace-two",
                                    "pod":                  "annotation",
                                },
                            },
                        },
                    },
                },
                &appsv1.Deployment{
                    TypeMeta: metav1.TypeMeta{
                        Kind:       "Deployment",
                        APIVersion: appsv1.SchemeGroupVersion.String(),
                    },
                    ObjectMeta: metav1.ObjectMeta{
                        Namespace: "install-namespace",
                        Name:      "deployment-two",
                    },
                    Spec: appsv1.DeploymentSpec{
                        RevisionHistoryLimit: ptr.To(int32(1)),
                        Template: corev1.PodTemplateSpec{
                            ObjectMeta: metav1.ObjectMeta{
                                Annotations: map[string]string{
                                    "csv":                  "annotation",
                                    "olm.targetNamespaces": "watch-namespace-one,watch-namespace-two",
                                },
                            },
                        },
                    },
                },
            },
        },
    } {
        t.Run(tc.name, func(t *testing.T) {
            objs, err := render.BundleDeploymentGenerator(tc.bundle, tc.opts)
            require.NoError(t, err)
            require.Equal(t, tc.expectedResources, objs)
        })
    }
}

func Test_BundlePermissionsGenerator_AllNamespace_Succeeds(t *testing.T) {
    opts := render.Options{
        InstallNamespace: "install-namespace",
        TargetNamespaces: []string{""},
        UniqueNameGenerator: func(name string, obj interface{}) (string, error) {
            return name, nil
        },
    }
    bundle := &convert.RegistryV1{
        CSV: MakeCSV(
            WithName("bob"),
            WithPermissions(
                v1alpha1.StrategyDeploymentPermissions{
                    ServiceAccountName: "service-account-one",
                    Rules: []rbacv1.PolicyRule{
                        {
                            APIGroups: []string{""},
                            Resources: []string{"namespaces"},
                            Verbs:     []string{"get", "list", "watch"},
                        },
                    },
                },
                v1alpha1.StrategyDeploymentPermissions{
                    ServiceAccountName: "service-account-two",
                    Rules: []rbacv1.PolicyRule{
                        {
                            APIGroups: []string{"appsv1"},
                            Resources: []string{"deployments"},
                            Verbs:     []string{"delete"},
                        },
                    },
                },
            ),
            WithClusterPermissions(
                v1alpha1.StrategyDeploymentPermissions{
                    ServiceAccountName: "service-account-one",
                    Rules: []rbacv1.PolicyRule{
                        {
                            APIGroups: []string{"rbacv1"},
                            Resources: []string{"clusterroles"},
                            Verbs:     []string{"create"},
                        },
                    },
                },
            ),
        ),
    }

    objs, err := render.BundlePermissionsGenerator(bundle, opts)
    require.NoError(t, err)
    require.Len(t, objs, 8)
}

func Test_BundlePermissionsGenerator_MultiNamespace_Succeeds(t *testing.T) {
    opts := render.Options{
        InstallNamespace: "install-namespace",
        TargetNamespaces: []string{"ns-one", "ns-two"},
        UniqueNameGenerator: func(name string, obj interface{}) (string, error) {
            return name, nil
        },
    }
    bundle := &convert.RegistryV1{
        CSV: MakeCSV(
            WithName("bob"),
            WithPermissions(
                v1alpha1.StrategyDeploymentPermissions{
                    ServiceAccountName: "service-account-one",
                    Rules: []rbacv1.PolicyRule{
                        {
                            APIGroups: []string{""},
                            Resources: []string{"namespaces"},
                            Verbs:     []string{"get", "list", "watch"},
                        },
                    },
                },
                v1alpha1.StrategyDeploymentPermissions{
                    ServiceAccountName: "service-account-two",
                    Rules: []rbacv1.PolicyRule{
                        {
                            APIGroups: []string{"appsv1"},
                            Resources: []string{"deployments"},
                            Verbs:     []string{"delete"},
                        },
                    },
                },
            ),
            WithClusterPermissions(
                v1alpha1.StrategyDeploymentPermissions{
                    ServiceAccountName: "service-account-one",
                    Rules: []rbacv1.PolicyRule{
                        {
                            APIGroups: []string{"rbacv1"},
                            Resources: []string{"clusterroles"},
                            Verbs:     []string{"create"},
                        },
                    },
                },
            ),
        ),
    }

    objs, err := render.BundlePermissionsGenerator(bundle, opts)
    require.NoError(t, err)
    require.Len(t, objs, 12)
}

func Test_BundlePermissionsGenerator_SingleOwnNamespace_Succeeds(t *testing.T) {
    opts := render.Options{
        InstallNamespace: "install-namespace",
        TargetNamespaces: []string{"install-namespace"},
        UniqueNameGenerator: func(name string, obj interface{}) (string, error) {
            return name, nil
        },
    }
    bundle := &convert.RegistryV1{
        CSV: MakeCSV(
            WithName("bob"),
            WithPermissions(
                v1alpha1.StrategyDeploymentPermissions{
                    ServiceAccountName: "service-account-one",
                    Rules: []rbacv1.PolicyRule{
                        {
                            APIGroups: []string{""},
                            Resources: []string{"namespaces"},
                            Verbs:     []string{"get", "list", "watch"},
                        },
                    },
                },
                v1alpha1.StrategyDeploymentPermissions{
                    ServiceAccountName: "service-account-two",
                    Rules: []rbacv1.PolicyRule{
                        {
                            APIGroups: []string{"appsv1"},
                            Resources: []string{"deployments"},
                            Verbs:     []string{"delete"},
                        },
                    },
                },
            ),
            WithClusterPermissions(
                v1alpha1.StrategyDeploymentPermissions{
                    ServiceAccountName: "service-account-one",
                    Rules: []rbacv1.PolicyRule{
                        {
                            APIGroups: []string{"rbacv1"},
                            Resources: []string{"clusterroles"},
                            Verbs:     []string{"create"},
                        },
                    },
                },
            ),
        ),
    }

    objs, err := render.BundlePermissionsGenerator(bundle, opts)
    require.NoError(t, err)
    require.Len(t, objs, 8)
}

func Test_BundleCRDGenerator_Succeeds(t *testing.T) {
    opts := render.Options{
        InstallNamespace: "install-namespace",
        TargetNamespaces: []string{""},
    }

    bundle := &convert.RegistryV1{
        CRDs: []apiextensionsv1.CustomResourceDefinition{
            {ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
            {ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
        },
    }

    objs, err := render.BundleCRDGenerator(bundle, opts)
    require.NoError(t, err)
    require.Equal(t, objs, []client.Object{
        &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "crd-one"}},
        &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "crd-two"}},
    })
}

func Test_BundleResourceGenerator_Succeeds(t *testing.T) {
    opts := render.Options{
        InstallNamespace: "install-namespace",
    }

    bundle := &convert.RegistryV1{
        Others: []unstructured.Unstructured{
            toUnstructured(
                &corev1.Service{
                    TypeMeta: metav1.TypeMeta{
                        Kind:       "Service",
                        APIVersion: corev1.SchemeGroupVersion.String(),
                    },
                    ObjectMeta: metav1.ObjectMeta{
                        Name: "bundled-service",
                    },
                },
            ),
            toUnstructured(
                &rbacv1.ClusterRole{
                    TypeMeta: metav1.TypeMeta{
                        Kind:       "ClusterRole",
                        APIVersion: rbacv1.SchemeGroupVersion.String(),
                    },
                    ObjectMeta: metav1.ObjectMeta{
                        Name: "bundled-clusterrole",
                    },
                },
            ),
        },
    }

    objs, err := render.BundleResourceGenerator(bundle, opts)
    require.NoError(t, err)
    require.Len(t, objs, 2)

}

type CSVOption func(version *v1alpha1.ClusterServiceVersion)

func WithName(name string) CSVOption {
    return func(csv *v1alpha1.ClusterServiceVersion) {
        csv.Name = name
    }
}

func WithStrategyDeploymentSpecs(strategyDeploymentSpecs ...v1alpha1.StrategyDeploymentSpec) CSVOption {
    return func(csv *v1alpha1.ClusterServiceVersion) {
        csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs = strategyDeploymentSpecs
    }
}

func WithOwnedCRDs(crdDesc ...v1alpha1.CRDDescription) CSVOption {
    return func(csv *v1alpha1.ClusterServiceVersion) {
        csv.Spec.CustomResourceDefinitions.Owned = crdDesc
    }
}

func WithAnnotations(annotations map[string]string) CSVOption {
    return func(csv *v1alpha1.ClusterServiceVersion) {
        csv.Annotations = annotations
    }
}

func WithPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) CSVOption {
    return func(csv *v1alpha1.ClusterServiceVersion) {
        csv.Spec.InstallStrategy.StrategySpec.Permissions = permissions
    }
}

func WithClusterPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) CSVOption {
    return func(csv *v1alpha1.ClusterServiceVersion) {
        csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions = permissions
    }
}

func toUnstructured(obj client.Object) unstructured.Unstructured {
    gvk := obj.GetObjectKind().GroupVersionKind()

    var u unstructured.Unstructured
    uObj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
    unstructured.RemoveNestedField(uObj, "metadata", "creationTimestamp")
    unstructured.RemoveNestedField(uObj, "status")
    u.Object = uObj
    u.SetGroupVersionKind(gvk)
    return u
}

func MakeCSV(opts ...CSVOption) v1alpha1.ClusterServiceVersion {
    csv := v1alpha1.ClusterServiceVersion{}
    for _, opt := range opts {
        opt(&csv)
    }
    return csv
}
