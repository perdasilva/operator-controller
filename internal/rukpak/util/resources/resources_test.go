package resources_test

import (
	"github.com/operator-framework/operator-controller/internal/rukpak/util/resources"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewServiceAccount(t *testing.T) {
	namespace := "test-namespace"
	name := "test-service-account"
	want := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	got := resources.NewServiceAccount(namespace, name)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewServiceAccount() = %v, want %v", got, want)
	}
}

func TestNewRole(t *testing.T) {
	namespace := "test-namespace"
	name := "test-role"
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{"", "extensions"},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
	want := &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Rules: rules,
	}
	got := resources.NewRole(namespace, name, rules)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewRole() = %v, want %v", got, want)
	}
}

func TestNewNamespace(t *testing.T) {
	name := "test-namespace"
	want := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	got := resources.NewNamespace(name)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewNamespace() = %v, want %v", got, want)
	}
}

func TestNewClusterRole(t *testing.T) {
	name := "test-cluster-role"
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{"", "extensions"},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
	want := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: rules,
	}
	got := resources.NewClusterRole(name, rules)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewClusterRole() = %v, want %v", got, want)
	}
}

func TestNewRoleBinding(t *testing.T) {
	namespace := "test-namespace"
	name := "test-role-binding"
	roleName := "test-role"
	saNamespace := "test-sa-namespace"
	saNames := []string{"sa1", "sa2"}
	want := &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Namespace: saNamespace,
				Name:      "sa1",
			},
			{
				Kind:      "ServiceAccount",
				Namespace: saNamespace,
				Name:      "sa2",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		},
	}
	got := resources.NewRoleBinding(namespace, name, roleName, saNamespace, saNames...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewRoleBinding() = %v, want %v", got, want)
	}
}

func TestNewClusterRoleBinding(t *testing.T) {
	name := "test-cluster-role-binding"
	roleName := "test-cluster-role"
	saNamespace := "test-sa-namespace"
	saNames := []string{"sa1", "sa2"}
	want := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Namespace: saNamespace,
				Name:      "sa1",
			},
			{
				Kind:      "ServiceAccount",
				Namespace: saNamespace,
				Name:      "sa2",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     roleName,
		},
	}
	got := resources.NewClusterRoleBinding(name, roleName, saNamespace, saNames...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewClusterRoleBinding() = %v, want %v", got, want)
	}
}
