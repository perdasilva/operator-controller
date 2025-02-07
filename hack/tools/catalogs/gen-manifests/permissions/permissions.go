package permissions

import (
	"fmt"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

var (
	unnamedResourceVerbs = []string{"create", "list", "watch"}
	namedResourceVerbs   = []string{"get", "update", "patch", "delete"}
)

func GeneratePolicyRules(objs []client.Object) []rbacv1.PolicyRule {
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

//func GenerateInstallerRBAC(objs []client.Object) []client.Object {
//	installerRoleRuleSet := rbacMetaRuleSet{}
//	installerClusterRoleRuleSet := rbacMetaRuleSet{}
//	var controllerRoleRules []rbacv1.PolicyRule
//	var controllerClusterRoleRules []rbacv1.PolicyRule
//	for _, obj := range objs {
//		if obj.GetNamespace() == "" {
//			installerClusterRoleRuleSet.add(newPolicyRule(obj))
//			if cr, ok := obj.(*rbacv1.ClusterRole); ok {
//				controllerClusterRoleRules = append(controllerClusterRoleRules, cr.Rules...)
//			}
//		} else {
//			installerRoleRuleSet.add(newPolicyRule(obj))
//			if r, ok := obj.(*rbacv1.Role); ok {
//				controllerRoleRules = append(controllerRoleRules, r.Rules...)
//			}
//		}
//	}
//	return []client.Object{
//		newClusterRole("installer-cluster-role", installerClusterRoleRuleSet.generate()),
//		newRole("installer-role", installerRoleRuleSet.generate()),
//		newClusterRole("controller-cluster-role", controllerClusterRoleRules),
//		newRole("controller-role", controllerRoleRules),
//	}
//}

func newSetBlockOwnerDeletionOwnerReferencesPolicyRule(clusterExtensionName string) rbacv1.PolicyRule {
	return rbacv1.PolicyRule{
		APIGroups:     []string{"olm.operatorframework.io"},
		Resources:     []string{"clusterextensions/finalizers"},
		Verbs:         []string{"update", "list", "watch"},
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

func newClusterRole(name string, rules []rbacv1.PolicyRule) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: rules,
	}
}

func newRole(name string, rules []rbacv1.PolicyRule) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: rules,
	}
}
