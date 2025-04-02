package convert

import (
    "cmp"
    "fmt"
    "slices"
    "strconv"
    "strings"

    admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    rbacv1 "k8s.io/api/rbac/v1"
    apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/util/intstr"
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

type Options struct {
    InstallNamespace string
    TargetNamespaces []string
}

func (c Converter) Convert(rv1 RegistryV1, installNamespace string, targetNamespaces []string) (*Plain, error) {
    if err := c.BundleValidator.Validate(&rv1); err != nil {
        return nil, err
    }

    if installNamespace == "" {
        installNamespace = getDefaultInstallNamespace(&rv1)
    }

    if err := checkBundleSupport(&rv1, installNamespace, targetNamespaces); err != nil {
        return nil, err
    }

    opts := Options{
        InstallNamespace: installNamespace,
        TargetNamespaces: targetNamespaces,
    }

    // generate deployments
    deployments := map[string]*appsv1.Deployment{}
    serviceAccounts := map[string]corev1.ServiceAccount{}
    for _, depSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
        deployments[depSpec.Name] = renderDeployment(&rv1, depSpec, opts)
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

    roles := make([]rbacv1.Role, 0, len(targetNamespaces)*len(permissions))
    roleBindings := make([]rbacv1.RoleBinding, 0, len(targetNamespaces)*len(permissions))
    clusterRoles := make([]rbacv1.ClusterRole, 0, len(targetNamespaces)*len(clusterPermissions))
    clusterRoleBindings := make([]rbacv1.ClusterRoleBinding, 0, len(targetNamespaces)*len(clusterPermissions))

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

    // collect crds
    crds := map[string]*apiextensionsv1.CustomResourceDefinition{}
    for _, crd := range rv1.CRDs {
        crds[crd.Name] = crd.DeepCopy()
    }

    // generate webhook configurations
    validatingWebhooksByDeployment := map[string][]*admissionregistrationv1.ValidatingWebhookConfiguration{}
    mutatingWebhooksByDeployment := map[string][]*admissionregistrationv1.MutatingWebhookConfiguration{}
    conversionWebhooksByCRDByDeployment := map[string]map[string]apiextensionsv1.CustomResourceConversion{}
    webhookServicePortsByDeployment := map[string]sets.Set[corev1.ServicePort]{}
    for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
        // collect webhook service ports
        if _, ok := webhookServicePortsByDeployment[wh.DeploymentName]; !ok {
            webhookServicePortsByDeployment[wh.DeploymentName] = sets.Set[corev1.ServicePort]{}
        }
        webhookServicePortsByDeployment[wh.DeploymentName].Insert(getWebhookServicePort(wh))

        // collect webhook configurations and crd conversions
        switch wh.Type {
        case v1alpha1.ValidatingAdmissionWebhook:
            // generate validatingwebhookconfigurations
            validatingWebhooksByDeployment[wh.DeploymentName] = append(validatingWebhooksByDeployment[wh.DeploymentName], &admissionregistrationv1.ValidatingWebhookConfiguration{
                TypeMeta: metav1.TypeMeta{
                    Kind:       "ValidatingWebhookConfiguration",
                    APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
                },
                ObjectMeta: metav1.ObjectMeta{
                    Name: wh.GenerateName,
                },
                Webhooks: []admissionregistrationv1.ValidatingWebhook{
                    {
                        Name:                    wh.GenerateName,
                        Rules:                   wh.Rules,
                        FailurePolicy:           wh.FailurePolicy,
                        MatchPolicy:             wh.MatchPolicy,
                        ObjectSelector:          wh.ObjectSelector,
                        SideEffects:             wh.SideEffects,
                        TimeoutSeconds:          wh.TimeoutSeconds,
                        AdmissionReviewVersions: wh.AdmissionReviewVersions,
                        ClientConfig: admissionregistrationv1.WebhookClientConfig{
                            Service: &admissionregistrationv1.ServiceReference{
                                Namespace: installNamespace,
                                Name:      getWebhookServiceName(wh.DeploymentName),
                                Path:      wh.WebhookPath,
                                Port:      &wh.ContainerPort,
                            },
                        },
                    },
                },
            })
        case v1alpha1.MutatingAdmissionWebhook:
            mutatingWebhooksByDeployment[wh.DeploymentName] = append(mutatingWebhooksByDeployment[wh.DeploymentName], &admissionregistrationv1.MutatingWebhookConfiguration{
                TypeMeta: metav1.TypeMeta{
                    Kind:       "MutatingWebhookConfiguration",
                    APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
                },
                ObjectMeta: metav1.ObjectMeta{
                    Name: wh.GenerateName,
                },
                Webhooks: []admissionregistrationv1.MutatingWebhook{
                    {
                        Name:                    wh.GenerateName,
                        Rules:                   wh.Rules,
                        FailurePolicy:           wh.FailurePolicy,
                        MatchPolicy:             wh.MatchPolicy,
                        ObjectSelector:          wh.ObjectSelector,
                        SideEffects:             wh.SideEffects,
                        TimeoutSeconds:          wh.TimeoutSeconds,
                        AdmissionReviewVersions: wh.AdmissionReviewVersions,
                        ClientConfig: admissionregistrationv1.WebhookClientConfig{
                            Service: &admissionregistrationv1.ServiceReference{
                                Namespace: installNamespace,
                                Name:      getWebhookServiceName(wh.DeploymentName),
                                Path:      wh.WebhookPath,
                                Port:      &wh.ContainerPort,
                            },
                        },
                        ReinvocationPolicy: wh.ReinvocationPolicy,
                    },
                },
            })
        case v1alpha1.ConversionWebhook:
            if _, ok := conversionWebhooksByCRDByDeployment[wh.DeploymentName]; !ok {
                conversionWebhooksByCRDByDeployment[wh.DeploymentName] = map[string]apiextensionsv1.CustomResourceConversion{}
            }

            conversionWebhookPath := "/"
            if wh.WebhookPath != nil {
                conversionWebhookPath = *wh.WebhookPath
            }

            crdConversionMap := conversionWebhooksByCRDByDeployment[wh.DeploymentName]
            for _, conversionCRD := range wh.ConversionCRDs {
                crdConversionMap[conversionCRD] = apiextensionsv1.CustomResourceConversion{
                    Strategy: apiextensionsv1.WebhookConverter,
                    Webhook: &apiextensionsv1.WebhookConversion{
                        ClientConfig: &apiextensionsv1.WebhookClientConfig{
                            Service: &apiextensionsv1.ServiceReference{
                                Namespace: installNamespace,
                                Name:      getWebhookServiceName(wh.DeploymentName),
                                Path:      &conversionWebhookPath,
                                Port:      &wh.ContainerPort,
                            },
                        },
                        ConversionReviewVersions: wh.AdmissionReviewVersions,
                    },
                }
            }
        }
    }

    // generate webhook services
    webhookServices := map[string]*corev1.Service{}
    for depName, servicePorts := range webhookServicePortsByDeployment {
        ports := servicePorts.UnsortedList()
        slices.SortFunc(ports, func(a, b corev1.ServicePort) int {
            return cmp.Compare(a.Name, b.Name)
        })
        webhookServices[depName] = &corev1.Service{
            TypeMeta: metav1.TypeMeta{
                Kind:       "Service",
                APIVersion: corev1.SchemeGroupVersion.String(),
            },
            ObjectMeta: metav1.ObjectMeta{
                Name: getWebhookServiceName(depName),
            },
            Spec: corev1.ServiceSpec{
                Ports:    ports,
                Selector: deployments[depName].Spec.Selector.MatchLabels,
            },
        }
    }

    // update crd conversion webhook settings if they already exist
    whServiceToCrdsMap := map[string][]*apiextensionsv1.CustomResourceDefinition{}
    for crdName, _ := range crds {
        crd := crds[crdName]
        if crd.Spec.Conversion != nil && crd.Spec.Conversion.Webhook != nil && crd.Spec.Conversion.Webhook.ClientConfig != nil && crd.Spec.Conversion.Webhook.ClientConfig.Service != nil {
            crd.Spec.Conversion.Webhook.ClientConfig.Service.Namespace = installNamespace
            serviceName := crd.Spec.Conversion.Webhook.ClientConfig.Service.Name
            if _, ok := whServiceToCrdsMap[serviceName]; !ok {
                whServiceToCrdsMap[serviceName] = []*apiextensionsv1.CustomResourceDefinition{}
            }
            whServiceToCrdsMap[serviceName] = append(whServiceToCrdsMap[serviceName], crd)
        }
    }

    // update conversion webhook crds
    for _, crdConversionMap := range conversionWebhooksByCRDByDeployment {
        for crdName, crdConversion := range crdConversionMap {
            crd := crds[crdName]
            crd.Spec.Conversion = &crdConversion
        }
    }

    var others []client.Object
    for _, obj := range rv1.Others {
        obj := obj
        supported, namespaced := registrybundle.IsSupported(obj.GetKind())
        if !supported {
            return nil, fmt.Errorf("bundle contains unsupported resource: Name: %v, Kind: %v", obj.GetName(), obj.GetKind())
        }
        if namespaced {
            obj.SetNamespace(installNamespace)
        }
        others = append(others, &obj)
    }

    // apply certificates
    // note: webhookServices contains all deployments with at least one webhook definition
    var additionalCertObjs []client.Object
    for depName, webhookService := range webhookServices {
        // update deployment volume mounts
        c := CertManagerProvider{
            CertName:           fmt.Sprintf("%s-%s-cert", rv1.CSV.Name, depName),
            WebhookServiceName: getWebhookServiceName(depName),
            InstallNamespace:   installNamespace,
        }

        for _, wh := range validatingWebhooksByDeployment[depName] {
            if err := c.InjectCABundle(wh); err != nil {
                return nil, fmt.Errorf("failed to inject CA bundle: %w", err)
            }
        }
        for _, wh := range mutatingWebhooksByDeployment[depName] {
            if err := c.InjectCABundle(wh); err != nil {
                return nil, fmt.Errorf("failed to inject CA bundle: %w", err)
            }
        }
        for crdName := range conversionWebhooksByCRDByDeployment[depName] {
            crd := crds[crdName]
            if err := c.InjectCABundle(crd); err != nil {
                return nil, fmt.Errorf("failed to inject CA bundle: %w", err)
            }
        }

        if err := c.InjectCABundle(webhookService); err != nil {
            return nil, fmt.Errorf("failed to inject CA bundle: %w", err)
        }

        updateDeploymentVolumeMounts(deployments[depName], c.CertName, "tls.crt", "tls.key")

        addObjs, err := c.AdditionalObjects()
        if err != nil {
            return nil, fmt.Errorf("failed to inject addObjs: %w", err)
        }
        for _, obj := range addObjs {
            additionalCertObjs = append(additionalCertObjs, &obj)
        }
    }

    // apply certificates to bundle provided conversion webhook resources
    for whServiceName, crds := range whServiceToCrdsMap {
        c := CertManagerProvider{
            CertName:           fmt.Sprintf("%s-%s-cert", rv1.CSV.Name, whServiceName),
            WebhookServiceName: whServiceName,
            InstallNamespace:   installNamespace,
        }
        for idx, _ := range crds {
            if err := c.InjectCABundle(crds[idx]); err != nil {
                return nil, fmt.Errorf("failed to inject CA bundle: %w", err)
            }
        }

        for idx, _ := range others {
            if others[idx].GetObjectKind().GroupVersionKind().Kind == "Service" && others[idx].GetName() == whServiceName {
                if err := c.InjectCABundle(others[idx]); err != nil {
                    return nil, fmt.Errorf("failed to inject CA bundle: %w", err)
                }
            }
        }

        addObjs, err := c.AdditionalObjects()
        if err != nil {
            return nil, fmt.Errorf("failed to inject addObjs: %w", err)
        }
        for _, obj := range addObjs {
            additionalCertObjs = append(additionalCertObjs, &obj)
        }
    }

    //nolint:prealloc
    var objs []client.Object
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
    for _, obj := range crds {
        objs = append(objs, obj)
    }
    for _, obj := range webhookServices {
        objs = append(objs, obj)
    }
    for _, valWebhooks := range validatingWebhooksByDeployment {
        for _, obj := range valWebhooks {
            objs = append(objs, obj)
        }
    }
    for _, muteWebhooks := range mutatingWebhooksByDeployment {
        for _, obj := range muteWebhooks {
            objs = append(objs, obj)
        }
    }
    for _, obj := range additionalCertObjs {
        objs = append(objs, obj)
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
        objs = append(objs, obj)
    }
    return &Plain{Objects: objs}, nil
}

func renderDeployment(rv1 *RegistryV1, depSpec v1alpha1.StrategyDeploymentSpec, opts Options) *appsv1.Deployment {
    annotations := util.MergeMaps(rv1.CSV.Annotations, depSpec.Spec.Template.Annotations)
    annotations["olm.targetNamespaces"] = strings.Join(opts.TargetNamespaces, ",")
    depSpec.Spec.Template.Annotations = annotations
    return &appsv1.Deployment{
        TypeMeta: metav1.TypeMeta{
            Kind:       "Deployment",
            APIVersion: appsv1.SchemeGroupVersion.String(),
        },
        ObjectMeta: metav1.ObjectMeta{
            Namespace: opts.InstallNamespace,
            Name:      depSpec.Name,
            Labels:    depSpec.Label,
        },
        Spec: depSpec.Spec,
    }
}

func renderServiceAccount(_ *RegistryV1, perms v1alpha1.StrategyDeploymentPermissions, opts Options) *corev1.ServiceAccount {
    serviceAccount := newServiceAccount(opts.InstallNamespace, saNameOrDefault(perms.ServiceAccountName))
    return &serviceAccount
}

func getDefaultInstallNamespace(rv1 *RegistryV1) string {
    defaultInstallNamespace := rv1.CSV.Annotations["operatorframework.io/suggested-namespace"]
    if defaultInstallNamespace == "" {
        defaultInstallNamespace = fmt.Sprintf("%s-system", rv1.PackageName)
    }
    return defaultInstallNamespace
}

func getWebhookServiceName(deploymentName string) string {
    return fmt.Sprintf("%s-service", strings.ReplaceAll(deploymentName, ".", "-"))
}

func getWebhookServicePort(wh v1alpha1.WebhookDescription) corev1.ServicePort {
    containerPort := int32(443)
    if wh.ContainerPort > 0 {
        containerPort = wh.ContainerPort
    }

    targetPort := intstr.FromInt32(containerPort)
    if wh.TargetPort != nil {
        targetPort = *wh.TargetPort
    }

    return corev1.ServicePort{
        Name:       strconv.Itoa(int(containerPort)),
        Port:       containerPort,
        TargetPort: targetPort,
    }
}

func updateDeploymentVolumeMounts(dep *appsv1.Deployment, secretName string, certKey string, privKeyKey string) {
    // Need to inject volumes and volume mounts for the cert secret
    volumeNames := []string{"apiservice-cert", "webhook-cert"}
    dep.Spec.Template.Spec.Volumes = slices.DeleteFunc(dep.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
        return slices.Contains(volumeNames, v.Name)
    })
    dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes,
        corev1.Volume{
            Name: "apiservice-cert",
            VolumeSource: corev1.VolumeSource{
                Secret: &corev1.SecretVolumeSource{
                    SecretName: secretName,
                    Items: []corev1.KeyToPath{
                        {
                            Key:  certKey,
                            Path: "apiserver.crt",
                        },
                        {
                            Key:  privKeyKey,
                            Path: "apiserver.key",
                        },
                    },
                },
            },
        },
        corev1.Volume{
            Name: "webhook-cert",
            VolumeSource: corev1.VolumeSource{
                Secret: &corev1.SecretVolumeSource{
                    SecretName: secretName,
                    Items: []corev1.KeyToPath{
                        {
                            Key:  certKey,
                            Path: "tls.crt",
                        },
                        {
                            Key:  privKeyKey,
                            Path: "tls.key",
                        },
                    },
                },
            },
        },
    )

    volumeMounts := []corev1.VolumeMount{
        {Name: "apiservice-cert", MountPath: "/apiserver.local.config/certificates"},
        {Name: "webhook-cert", MountPath: "/tmp/k8s-webhook-server/serving-certs"},
    }
    for i := range dep.Spec.Template.Spec.Containers {
        dep.Spec.Template.Spec.Containers[i].VolumeMounts = slices.DeleteFunc(dep.Spec.Template.Spec.Containers[i].VolumeMounts, func(vm corev1.VolumeMount) bool {
            return slices.Contains(volumeNames, vm.Name)
        })
        dep.Spec.Template.Spec.Containers[i].VolumeMounts = append(dep.Spec.Template.Spec.Containers[i].VolumeMounts, volumeMounts...)
    }
}

func checkBundleSupport(rv1 *RegistryV1, installNamespace string, targetNamespaces []string) error {
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
        return err
    }

    if len(rv1.CSV.Spec.APIServiceDefinitions.Owned) > 0 {
        return fmt.Errorf("apiServiceDefintions are not supported")
    }

    //if len(rv1.CSV.Spec.WebhookDefinitions) > 0 {
    //	return fmt.Errorf("webhookDefinitions are not supported")
    //}

    return nil
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
