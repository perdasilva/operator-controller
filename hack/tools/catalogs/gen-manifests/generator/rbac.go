package generator

import (
	"fmt"
	"github.com/operator-framework/operator-controller/internal/rukpak/convert"
	filter "github.com/operator-framework/operator-controller/internal/util/slices"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"slices"
	"strings"
)

var (
	unnamedResourceVerbs   = []string{"create", "list", "watch"}
	namedResourceVerbs     = []string{"get", "update", "patch", "delete"}
	clusterScopedResources = []string{
		"ClusterRole",
		"ClusterRoleBinding",
		"PriorityClass",
		"ConsoleYAMLSample",
		"ConsoleQuickStart",
		"ConsoleCLIDownload",
		"ConsoleLink",
		"CustomResourceDefinition",
	}
	namespaceScopedResources = []string{
		"Secret",
		"ConfigMap",
		"ServiceAccount",
		"Service",
		"Role",
		"RoleBinding",
		"PrometheusRule",
		"ServiceMonitor",
		"PodDisruptionBudget",
		"VerticalPodAutoscaler",
		"Deployment",
	}
)

var isNamespacedResource filter.Predicate[client.Object] = func(o client.Object) bool {
	return slices.Contains(namespaceScopedResources, o.GetObjectKind().GroupVersionKind().Kind)
}

var isClusterScopedResource filter.Predicate[client.Object] = func(o client.Object) bool {
	return slices.Contains(clusterScopedResources, o.GetObjectKind().GroupVersionKind().Kind)
}

var isClusterRole = isOfKind("ClusterRole")
var isRole = isOfKind("Role")

func isOfKind(kind string) filter.Predicate[client.Object] {
	return func(o client.Object) bool {
		return o.GetObjectKind().GroupVersionKind().Kind == kind
	}
}

func inNamespace(namespace string) filter.Predicate[client.Object] {
	return func(o client.Object) bool {
		return o.GetNamespace() == namespace
	}
}

var isGeneratedResource filter.Predicate[client.Object] = func(o client.Object) bool {
	annotations := o.GetAnnotations()
	_, exists := annotations[convert.AnnotationOLMGeneratedResource]
	return annotations != nil && exists
}

func generatePolicyRules(objs []client.Object) []rbacv1.PolicyRule {
	resourceNameMap := groupResourceNamesByGroupKind(objs)
	policyRules := make([]rbacv1.PolicyRule, 0, 2*len(resourceNameMap))
	for groupKind, resourceNames := range resourceNameMap {
		policyRules = append(policyRules, []rbacv1.PolicyRule{
			newPolicyRule(groupKind, unnamedResourceVerbs),
			newPolicyRule(groupKind, namedResourceVerbs, resourceNames...),
		}...)
	}
	return policyRules
}

func collectRBACResourcePolicyRules(objs []client.Object) []rbacv1.PolicyRule {
	var policyRules []rbacv1.PolicyRule
	for _, obj := range objs {
		if cr, ok := obj.(*rbacv1.ClusterRole); ok {
			policyRules = append(policyRules, cr.Rules...)
		} else if r, ok := obj.(*rbacv1.Role); ok {
			policyRules = append(policyRules, r.Rules...)
		} else {
			panic(fmt.Sprintf("unexpected type %T", obj))
		}
	}
	return policyRules
}

func newClusterExtensionFinalizerPolicyRule(clusterExtensionName string) rbacv1.PolicyRule {
	return rbacv1.PolicyRule{
		APIGroups:     []string{"olm.operatorframework.io"},
		Resources:     []string{"clusterextensions/finalizers"},
		Verbs:         []string{"update"},
		ResourceNames: []string{clusterExtensionName},
	}
}

func groupResourceNamesByGroupKind(objs []client.Object) map[schema.GroupKind][]string {
	resourceNames := map[schema.GroupKind][]string{}
	for _, obj := range objs {
		key := obj.GetObjectKind().GroupVersionKind().GroupKind()
		if _, ok := resourceNames[key]; !ok {
			resourceNames[key] = []string{}
		}
		resourceNames[key] = append(resourceNames[key], obj.GetName())
	}
	return resourceNames
}

func newPolicyRule(groupKind schema.GroupKind, verbs []string, resourceNames ...string) rbacv1.PolicyRule {
	return rbacv1.PolicyRule{
		APIGroups:     []string{groupKind.Group},
		Resources:     []string{fmt.Sprintf("%ss", strings.ToLower(groupKind.Kind))},
		Verbs:         verbs,
		ResourceNames: resourceNames,
	}
}
