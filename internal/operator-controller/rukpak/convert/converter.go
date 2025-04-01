package convert

import (
    "fmt"
    "strings"

    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    rbacv1 "k8s.io/api/rbac/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/util/sets"
    "sigs.k8s.io/controller-runtime/pkg/client"

    "github.com/operator-framework/api/pkg/operators/v1alpha1"
    registrybundle "github.com/operator-framework/operator-registry/pkg/lib/bundle"

    "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

const maxNameLength = 63

type Converter struct {
    BundleValidator BundleValidator
}

func (c Converter) Convert(rv1 RegistryV1, installNamespace string, targetNamespaces []string) (*Plain, error) {
    if err := c.BundleValidator.Validate(&rv1); err != nil {
        return nil, err
    }

    if installNamespace == "" {
        installNamespace = rv1.CSV.Annotations["operatorframework.io/suggested-namespace"]
    }
    if installNamespace == "" {
        installNamespace = fmt.Sprintf("%s-system", rv1.PackageName)
    }
    supportedInstallModes := sets.New[string]()
    for _, im := range rv1.CSV.Spec.InstallModes {
        if im.Supported {
            supportedInstallModes.Insert(string(im.Type))
        }
    }
    if len(targetNamespaces) == 0 {
        if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeAllNamespaces)) {
            targetNamespaces = []string{""}
        } else if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeOwnNamespace)) {
            targetNamespaces = []string{installNamespace}
        }
    }

    if err := validateTargetNamespaces(supportedInstallModes, installNamespace, targetNamespaces); err != nil {
        return nil, err
    }

    if len(rv1.CSV.Spec.APIServiceDefinitions.Owned) > 0 {
        return nil, fmt.Errorf("apiServiceDefintions are not supported")
    }

    if len(rv1.CSV.Spec.WebhookDefinitions) > 0 {
        return nil, fmt.Errorf("webhookDefinitions are not supported")
    }

    deployments := []appsv1.Deployment{}
    serviceAccounts := map[string]corev1.ServiceAccount{}
    for _, depSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
        annotations := util.MergeMaps(rv1.CSV.Annotations, depSpec.Spec.Template.Annotations)
        annotations["olm.targetNamespaces"] = strings.Join(targetNamespaces, ",")
        depSpec.Spec.Template.Annotations = annotations
        deployments = append(deployments, appsv1.Deployment{
            TypeMeta: metav1.TypeMeta{
                Kind:       "Deployment",
                APIVersion: appsv1.SchemeGroupVersion.String(),
            },

            ObjectMeta: metav1.ObjectMeta{
                Namespace: installNamespace,
                Name:      depSpec.Name,
                Labels:    depSpec.Label,
            },
            Spec: depSpec.Spec,
        })
        saName := saNameOrDefault(depSpec.Spec.Template.Spec.ServiceAccountName)
        serviceAccounts[saName] = newServiceAccount(installNamespace, saName)
    }

    // NOTES:
    //   1. There's an extra Role for OperatorConditions: get/update/patch; resourceName=csv.name
    //        - This is managed by the OperatorConditions controller here: https://github.com/operator-framework/operator-lifecycle-manager/blob/9ced412f3e263b8827680dc0ad3477327cd9a508/pkg/controller/operators/operatorcondition_controller.go#L106-L109
    //   2. There's an extra RoleBinding for the above mentioned role.
    //        - Every SA mentioned in the OperatorCondition.spec.serviceAccounts is a subject for this role binding: https://github.com/operator-framework/operator-lifecycle-manager/blob/9ced412f3e263b8827680dc0ad3477327cd9a508/pkg/controller/operators/operatorcondition_controller.go#L171-L177
    //   3. strategySpec.permissions are _also_ given a clusterrole/clusterrole binding.
    //  		- (for AllNamespaces mode only?)
    //			- (where does the extra namespaces get/list/watch rule come from?)

    roles := []rbacv1.Role{}
    roleBindings := []rbacv1.RoleBinding{}
    clusterRoles := []rbacv1.ClusterRole{}
    clusterRoleBindings := []rbacv1.ClusterRoleBinding{}

    permissions := rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions
    clusterPermissions := rv1.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions
    allPermissions := append(permissions, clusterPermissions...)

    // Create all the service accounts
    for _, permission := range allPermissions {
        saName := saNameOrDefault(permission.ServiceAccountName)
        if _, ok := serviceAccounts[saName]; !ok {
            serviceAccounts[saName] = newServiceAccount(installNamespace, saName)
        }
    }

    // If we're in AllNamespaces mode, promote the permissions to clusterPermissions
    if len(targetNamespaces) == 1 && targetNamespaces[0] == "" {
        for _, p := range permissions {
            p.Rules = append(p.Rules, rbacv1.PolicyRule{
                Verbs:     []string{"get", "list", "watch"},
                APIGroups: []string{corev1.GroupName},
                Resources: []string{"namespaces"},
            })
        }
        clusterPermissions = append(clusterPermissions, permissions...)
        permissions = nil
    }

    for _, ns := range targetNamespaces {
        for _, permission := range permissions {
            saName := saNameOrDefault(permission.ServiceAccountName)
            name, err := generateName(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)
            if err != nil {
                return nil, err
            }
            roles = append(roles, newRole(ns, name, permission.Rules))
            roleBindings = append(roleBindings, newRoleBinding(ns, name, name, installNamespace, saName))
        }
    }

    for _, permission := range clusterPermissions {
        saName := saNameOrDefault(permission.ServiceAccountName)
        name, err := generateName(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)
        if err != nil {
            return nil, err
        }
        clusterRoles = append(clusterRoles, newClusterRole(name, permission.Rules))
        clusterRoleBindings = append(clusterRoleBindings, newClusterRoleBinding(name, name, installNamespace, saName))
    }

    objs := []client.Object{}
    for _, obj := range serviceAccounts {
        obj := obj
        if obj.GetName() != "default" {
            objs = append(objs, &obj)
        }
    }
    for _, obj := range roles {
        obj := obj
        objs = append(objs, &obj)
    }
    for _, obj := range roleBindings {
        obj := obj
        objs = append(objs, &obj)
    }
    for _, obj := range clusterRoles {
        obj := obj
        objs = append(objs, &obj)
    }
    for _, obj := range clusterRoleBindings {
        obj := obj
        objs = append(objs, &obj)
    }
    for _, obj := range rv1.CRDs {
        objs = append(objs, &obj)
    }
    for _, obj := range rv1.Others {
        obj := obj
        supported, namespaced := registrybundle.IsSupported(obj.GetKind())
        if !supported {
            return nil, fmt.Errorf("bundle contains unsupported resource: Name: %v, Kind: %v", obj.GetName(), obj.GetKind())
        }
        if namespaced {
            obj.SetNamespace(installNamespace)
        }
        objs = append(objs, &obj)
    }
    for _, obj := range deployments {
        obj := obj
        objs = append(objs, &obj)
    }
    return &Plain{Objects: objs}, nil
}

func saNameOrDefault(saName string) string {
    if saName == "" {
        return "default"
    }
    return saName
}

func generateName(base string, o interface{}) (string, error) {
    hashStr, err := util.DeepHashObject(o)
    if err != nil {
        return "", err
    }
    if len(base)+len(hashStr) > maxNameLength {
        base = base[:maxNameLength-len(hashStr)-1]
    }

    return fmt.Sprintf("%s-%s", base, hashStr), nil
}

func newServiceAccount(namespace, name string) corev1.ServiceAccount {
    return corev1.ServiceAccount{
        TypeMeta: metav1.TypeMeta{
            Kind:       "ServiceAccount",
            APIVersion: corev1.SchemeGroupVersion.String(),
        },
        ObjectMeta: metav1.ObjectMeta{
            Namespace: namespace,
            Name:      name,
        },
    }
}

func newRole(namespace, name string, rules []rbacv1.PolicyRule) rbacv1.Role {
    return rbacv1.Role{
        TypeMeta: metav1.TypeMeta{
            Kind:       "Role",
            APIVersion: rbacv1.SchemeGroupVersion.String(),
        },
        ObjectMeta: metav1.ObjectMeta{
            Namespace: namespace,
            Name:      name,
        },
        Rules: rules,
    }
}

func newClusterRole(name string, rules []rbacv1.PolicyRule) rbacv1.ClusterRole {
    return rbacv1.ClusterRole{
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

func newRoleBinding(namespace, name, roleName, saNamespace string, saNames ...string) rbacv1.RoleBinding {
    subjects := make([]rbacv1.Subject, 0, len(saNames))
    for _, saName := range saNames {
        subjects = append(subjects, rbacv1.Subject{
            Kind:      "ServiceAccount",
            Namespace: saNamespace,
            Name:      saName,
        })
    }
    return rbacv1.RoleBinding{
        TypeMeta: metav1.TypeMeta{
            Kind:       "RoleBinding",
            APIVersion: rbacv1.SchemeGroupVersion.String(),
        },
        ObjectMeta: metav1.ObjectMeta{
            Namespace: namespace,
            Name:      name,
        },
        Subjects: subjects,
        RoleRef: rbacv1.RoleRef{
            APIGroup: rbacv1.GroupName,
            Kind:     "Role",
            Name:     roleName,
        },
    }
}

func newClusterRoleBinding(name, roleName, saNamespace string, saNames ...string) rbacv1.ClusterRoleBinding {
    subjects := make([]rbacv1.Subject, 0, len(saNames))
    for _, saName := range saNames {
        subjects = append(subjects, rbacv1.Subject{
            Kind:      "ServiceAccount",
            Namespace: saNamespace,
            Name:      saName,
        })
    }
    return rbacv1.ClusterRoleBinding{
        TypeMeta: metav1.TypeMeta{
            Kind:       "ClusterRoleBinding",
            APIVersion: rbacv1.SchemeGroupVersion.String(),
        },
        ObjectMeta: metav1.ObjectMeta{
            Name: name,
        },
        Subjects: subjects,
        RoleRef: rbacv1.RoleRef{
            APIGroup: rbacv1.GroupName,
            Kind:     "ClusterRole",
            Name:     roleName,
        },
    }
}
