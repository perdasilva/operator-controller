package generator

import (
	"context"
	"fmt"
	"github.com/operator-framework/operator-controller/internal/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/rukpak/util/resources"
	sliceutil "github.com/operator-framework/operator-controller/internal/util/slices"
	"io/fs"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"slices"
)

func GenerateManifests(ctx context.Context, bundleFS fs.FS, opts Options) ([]client.Object, error) {
	reg, err := convert.BundleFSToRegistryV1(ctx, bundleFS)
	if err != nil {
		return nil, fmt.Errorf("could not render bundle manifests: %w", err)
	}

	// set resource names
	if opts.clusterExtensionName == "" {
		opts.clusterExtensionName = reg.PackageName
	}
	if opts.installNamespace == "" {
		opts.installNamespace = fmt.Sprintf("%s-system", reg.PackageName)
	}
	opts.serviceAccountName = fmt.Sprintf("%s-installer", reg.PackageName)
	opts.clusterRoleName = fmt.Sprintf("%s-clusterrole", reg.PackageName)
	opts.clusterRoleBindingName = fmt.Sprintf("%s-clusterrole-binding", reg.PackageName)
	opts.roleName = fmt.Sprintf("%s-role", reg.PackageName)
	opts.roleBindingName = fmt.Sprintf("%s-role-binding", reg.PackageName)

	plain, err := convert.Convert(reg, opts.installNamespace, []string{opts.WatchNamespace})
	if err != nil {
		return nil, fmt.Errorf("could not render bundle manifests: %w", err)
	}
	return generateInstallerRBACManifests(plain.Objects, opts), nil
}

func generateInstallerRBACManifests(objs []client.Object, opts Options) []client.Object {
	rbacManifests := []client.Object{
		// installer service account
		resources.NewServiceAccount(
			opts.installNamespace,
			opts.serviceAccountName,
		),

		// cluster scoped resources
		resources.NewClusterRole(
			opts.clusterRoleName,
			slices.Concat(
				// finalizer rule
				[]rbacv1.PolicyRule{newClusterExtensionFinalizerPolicyRule(opts.clusterExtensionName)},
				// cluster scoped resource creation and management rules
				generatePolicyRules(sliceutil.Filter(objs, isClusterScopedResource)),
				// controller rbac scope
				collectRBACResourcePolicyRules(sliceutil.Filter(objs, isClusterRole, isGeneratedResource)),
			),
		),
		resources.NewClusterRoleBinding(
			opts.clusterRoleBindingName,
			opts.clusterRoleName,
			opts.installNamespace,
			opts.serviceAccountName,
		),

		// namespace scoped install namespace resources
		resources.NewRole(
			opts.installNamespace,
			opts.roleName,
			slices.Concat(
				// namespace scoped resource creation and management rules
				generatePolicyRules(sliceutil.Filter(objs, isNamespacedResource, inNamespace(opts.installNamespace))),
				// controller rbac scope
				collectRBACResourcePolicyRules(sliceutil.Filter(objs, isRole, isGeneratedResource, inNamespace(opts.installNamespace))),
			),
		),
		resources.NewRoleBinding(
			opts.installNamespace,
			opts.roleBindingName,
			opts.roleName,
			opts.installNamespace,
			opts.serviceAccountName,
		),

		// namespace scoped watch namespace resources
		resources.NewRole(
			opts.WatchNamespace,
			opts.roleName,
			slices.Concat(
				// namespace scoped resource creation and management rules
				generatePolicyRules(sliceutil.Filter(objs, isNamespacedResource, inNamespace(opts.WatchNamespace))),
				// controller rbac scope
				collectRBACResourcePolicyRules(sliceutil.Filter(objs, isRole, isGeneratedResource, inNamespace(opts.WatchNamespace))),
			),
		),
		resources.NewRoleBinding(
			opts.WatchNamespace,
			opts.roleBindingName,
			opts.roleName,
			opts.installNamespace,
			opts.serviceAccountName,
		),
	}

	// remove any cluster/role(s) without any defined rules and pair cluster/role add manifests
	return slices.DeleteFunc(rbacManifests, isNoRules)
}

func isNoRules(object client.Object) bool {
	switch obj := object.(type) {
	case *rbacv1.ClusterRole:
		return len(obj.Rules) == 0
	case *rbacv1.Role:
		return len(obj.Rules) == 0
	}
	return false
}
