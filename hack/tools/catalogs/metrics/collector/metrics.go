package collector

import (
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/property"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/sets"
	"slices"
)

const (
	labelCatalog = "catalog"
	labelPackage = "package"
	labelBundle  = "bundle"

	resourceSubscriptions      = "subscriptions"
	resourceOperatorConditions = "operatorconditions"

	featureDisconnected   = "features.operators.openshift.io/disconnected"
	featureFIPS           = "features.operators.openshift.io/fips-compliant"
	featureProxyAware     = "features.operators.openshift.io/proxy-aware"
	featureTLSProfiles    = "features.operators.penshift.io/tls-profiles"
	featureTokenAuthAWS   = "features.operators.openshift.io/token-auth-aws"
	featureTokenAuthAzure = "features.operators.openshift.io/token-auth-azure"
	featureTokenAuthGCP   = "features.operators.openshift.io/token-auth-gcp"
)

type CatalogMetrics struct {
	BundleTotal *prometheus.CounterVec

	HasValidatingWebhooksTotal *prometheus.CounterVec
	HasMutatingWebhooksTotal   *prometheus.CounterVec
	HasConversionWebhooksTotal *prometheus.CounterVec

	CRDCount *prometheus.CounterVec

	AllNamespacesSupportTotal   *prometheus.CounterVec
	MultiNamespaceSupportTotal  *prometheus.CounterVec
	OwnNamespaceSupportTotal    *prometheus.CounterVec
	SingleNamespaceSupportTotal *prometheus.CounterVec

	HasGVKRequiredPropertyTotal *prometheus.CounterVec
	HasPackageRequiredTotal     *prometheus.CounterVec

	HasAPIServiceTotal *prometheus.CounterVec

	HasV0OperatorConditionRBACTotal *prometheus.CounterVec
	HasV0SubscriptionRBACTotal      *prometheus.CounterVec

	SupportsFeatureDisconnectedTotal   *prometheus.CounterVec
	SupportsFeatureFIPSCompliantTotal  *prometheus.CounterVec
	SupportsFeatureProxyAwareTotal     *prometheus.CounterVec
	SupportsFeatureTLSProfilesTotal    *prometheus.CounterVec
	SupportsFeatureTokenAuthAWSTotal   *prometheus.CounterVec
	SupportsFeatureTokenAuthAzureTotal *prometheus.CounterVec
	SupportsFeatureTokenAuthGCPTotal   *prometheus.CounterVec

	BundleSupportsV1            *prometheus.CounterVec
	BundleSupportsV1TechPreview *prometheus.CounterVec

	NoAllNamespaceSupport *prometheus.CounterVec
}

func NewCatalogMetrics() *CatalogMetrics {
	return &CatalogMetrics{
		BundleTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "catalog_bundle_total",
			Help: "Total number of bundles in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		HasValidatingWebhooksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_has_validating_webhooks_total",
			Help: "Total number of bundles that contain validating webhooks in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		HasMutatingWebhooksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_has_mutating_webhooks_total",
			Help: "Total number of bundles that contain mutating webhooks in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		HasConversionWebhooksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_has_conversion_webhooks_total",
			Help: "Total number of bundles that contain validating webhooks in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		CRDCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_crd_count",
			Help: "Total number of CRDs in a BundleFBC",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		AllNamespacesSupportTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_all_namespaces_support_total",
			Help: "Total number of bundles that support AllNamespace install mode in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		MultiNamespaceSupportTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_multi_namespace_support_total",
			Help: "Total number of bundles that support MultiNamespace install mode in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		OwnNamespaceSupportTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_own_namespace_support_total",
			Help: "Total number of bundles that support OwnNamespace install mode in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		SingleNamespaceSupportTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_single_namespace_support_total",
			Help: "Total number of bundles that support SingleNamespace install mode in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		HasGVKRequiredPropertyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_has_gvk_required_property_total",
			Help: "Total number of bundles that declare a gvk dependency in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		HasPackageRequiredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_has_package_required_total",
			Help: "Total number of bundles that declare a package dependency in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		HasAPIServiceTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_has_api_service_total",
			Help: "Total number of bundles that contain api services in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		HasV0OperatorConditionRBACTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_has_v0_operator_condition_rbac_total",
			Help: "Total number of bundles that contain OperatorCondition RBAC in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		HasV0SubscriptionRBACTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_has_v0_subscription_rbac_total",
			Help: "Total number of bundles that contain Subscription RBAC in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		SupportsFeatureDisconnectedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_supports_feature_disconnected_total",
			Help: "Total number of bundles that support FeatureDisconnected in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		SupportsFeatureFIPSCompliantTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_supports_feature_fips_compliant_total",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		SupportsFeatureProxyAwareTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_supports_feature_proxy_aware_total",
			Help: "Total number of bundles that support FeatureProxyAware in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		SupportsFeatureTLSProfilesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_supports_feature_tls_profiles_total",
			Help: "Total number of bundles that support FeatureTLSProfiles in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		SupportsFeatureTokenAuthAWSTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_supports_feature_token_auth_aws_total",
			Help: "Total number of bundles that support FeatureTokenAuthAWS in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		SupportsFeatureTokenAuthAzureTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_supports_feature_token_auth_azure_total",
			Help: "Total number of bundles that support FeatureTokenAuthAzure in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		SupportsFeatureTokenAuthGCPTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_supports_feature_token_auth_gcp_total",
			Help: "Total number of bundles that support FeatureTokenAuthGCP in a catalog",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		BundleSupportsV1: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_supports_v1_total",
			Help: "Total number of bundles that support v1",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		BundleSupportsV1TechPreview: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_supports_v1_techpreview_total",
			Help: "Total number of bundles that support v1",
		}, []string{labelCatalog, labelPackage, labelBundle}),
		NoAllNamespaceSupport: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bundle_no_all_namespaces_support_total",
			Help: "Total number of bundles that don't support all namespaces mode",
		}, []string{labelCatalog, labelPackage, labelBundle}),
	}
}

func (c *CatalogMetrics) RegisterMetrics(reg *prometheus.Registry) {
	reg.MustRegister(c.BundleTotal)
	reg.MustRegister(c.HasValidatingWebhooksTotal)
	reg.MustRegister(c.HasMutatingWebhooksTotal)
	reg.MustRegister(c.HasConversionWebhooksTotal)
	reg.MustRegister(c.CRDCount)
	reg.MustRegister(c.AllNamespacesSupportTotal)
	reg.MustRegister(c.MultiNamespaceSupportTotal)
	reg.MustRegister(c.OwnNamespaceSupportTotal)
	reg.MustRegister(c.SingleNamespaceSupportTotal)
	reg.MustRegister(c.HasGVKRequiredPropertyTotal)
	reg.MustRegister(c.HasPackageRequiredTotal)
	reg.MustRegister(c.HasAPIServiceTotal)
	reg.MustRegister(c.HasV0OperatorConditionRBACTotal)
	reg.MustRegister(c.HasV0SubscriptionRBACTotal)
	reg.MustRegister(c.SupportsFeatureDisconnectedTotal)
	reg.MustRegister(c.SupportsFeatureFIPSCompliantTotal)
	reg.MustRegister(c.SupportsFeatureProxyAwareTotal)
	reg.MustRegister(c.SupportsFeatureTLSProfilesTotal)
	reg.MustRegister(c.SupportsFeatureTokenAuthAWSTotal)
	reg.MustRegister(c.SupportsFeatureTokenAuthAzureTotal)
	reg.MustRegister(c.SupportsFeatureTokenAuthGCPTotal)
	reg.MustRegister(c.BundleSupportsV1)
	reg.MustRegister(c.BundleSupportsV1TechPreview)
	reg.MustRegister(c.NoAllNamespaceSupport)
}

func (c *CatalogMetrics) Update(bundle BundleUnpackResult) {
	var (
		hasDependencies               = false
		hasOperatorConditionsRBAC     = false
		hasAllNamespacesModeSupport   = false
		hasOwnNamespaceModeSupport    = false
		hasSingleNamespaceModeSupport = false
		hasWebhooks                   = false
		hasAPIServices                = false
	)

	bundleMetricLabels := prometheus.Labels{
		labelCatalog: bundle.CatalogName,
		labelPackage: bundle.Package,
		labelBundle:  bundle.Bundle.Name,
	}

	c.BundleTotal.With(bundleMetricLabels).Add(1)

	webhookTypes := sets.New[v1alpha1.WebhookAdmissionType]()
	for _, defn := range bundle.Resources.CSV.Spec.WebhookDefinitions {
		webhookTypes.Insert(defn.Type)
	}
	hasWebhooks = len(webhookTypes) > 0
	if webhookTypes.Has(v1alpha1.ValidatingAdmissionWebhook) {
		c.HasValidatingWebhooksTotal.With(bundleMetricLabels).Add(1)
	}
	if webhookTypes.Has(v1alpha1.MutatingAdmissionWebhook) {
		c.HasMutatingWebhooksTotal.With(bundleMetricLabels).Add(1)
	}
	if webhookTypes.Has(v1alpha1.ConversionWebhook) {
		c.HasConversionWebhooksTotal.With(bundleMetricLabels).Add(1)
	}

	if len(bundle.Resources.CSV.Spec.APIServiceDefinitions.Owned) > 0 {
		hasAPIServices = true
		c.HasAPIServiceTotal.With(bundleMetricLabels).Add(1)
	}

	bundlePropertyTypes := sets.New[string]()
	for _, defn := range bundle.Properties {
		bundlePropertyTypes.Insert(defn.Type)
	}
	if bundlePropertyTypes.Has(property.TypePackageRequired) {
		hasDependencies = true
		c.HasPackageRequiredTotal.With(bundleMetricLabels).Add(1)
	}
	if bundlePropertyTypes.Has(property.TypeGVKRequired) {
		hasDependencies = true
		c.HasGVKRequiredPropertyTotal.With(bundleMetricLabels).Add(1)
	}

	c.CRDCount.With(bundleMetricLabels).Add(float64(len(bundle.Resources.CRDs)))

	installModeTypes := sets.New[v1alpha1.InstallModeType]()
	for _, installMode := range bundle.Resources.CSV.Spec.InstallModes {
		if installMode.Supported {
			installModeTypes.Insert(installMode.Type)
		}
	}
	if installModeTypes.Has(v1alpha1.InstallModeTypeAllNamespaces) {
		hasAllNamespacesModeSupport = true
		c.AllNamespacesSupportTotal.With(bundleMetricLabels).Add(1)
	}
	if installModeTypes.Has(v1alpha1.InstallModeTypeSingleNamespace) {
		hasSingleNamespaceModeSupport = true
		c.SingleNamespaceSupportTotal.With(bundleMetricLabels).Add(1)
	}
	if installModeTypes.Has(v1alpha1.InstallModeTypeMultiNamespace) {
		c.MultiNamespaceSupportTotal.With(bundleMetricLabels).Add(1)
	}
	if installModeTypes.Has(v1alpha1.InstallModeTypeOwnNamespace) {
		hasOwnNamespaceModeSupport = true
		c.OwnNamespaceSupportTotal.With(bundleMetricLabels).Add(1)
	}

	rbacResources := sets.New[string]()
	for _, perm := range slices.Concat(bundle.Resources.CSV.Spec.InstallStrategy.StrategySpec.Permissions, bundle.Resources.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions) {
		for _, rule := range perm.Rules {
			for _, resource := range rule.Resources {
				rbacResources.Insert(resource)
			}
		}
	}
	if rbacResources.Has(resourceSubscriptions) {
		c.HasV0SubscriptionRBACTotal.With(bundleMetricLabels).Add(1)
	}
	if rbacResources.Has(resourceOperatorConditions) {
		hasOperatorConditionsRBAC = true
		c.HasV0OperatorConditionRBACTotal.With(bundleMetricLabels).Add(1)
	}

	supportedFeatures := sets.New[string]()
	for key, value := range bundle.Resources.CSV.Annotations {
		if value == "true" {
			supportedFeatures.Insert(key)
		}
	}
	if supportedFeatures.Has(featureDisconnected) {
		c.SupportsFeatureDisconnectedTotal.With(bundleMetricLabels).Add(1)
	}
	if supportedFeatures.Has(featureFIPS) {
		c.SupportsFeatureFIPSCompliantTotal.With(bundleMetricLabels).Add(1)
	}
	if supportedFeatures.Has(featureProxyAware) {
		c.SupportsFeatureProxyAwareTotal.With(bundleMetricLabels).Add(1)
	}
	if supportedFeatures.Has(featureTokenAuthAWS) {
		c.SupportsFeatureTokenAuthAWSTotal.With(bundleMetricLabels).Add(1)
	}
	if supportedFeatures.Has(featureTokenAuthAzure) {
		c.SupportsFeatureTokenAuthAzureTotal.With(bundleMetricLabels).Add(1)
	}
	if supportedFeatures.Has(featureTokenAuthGCP) {
		c.SupportsFeatureTokenAuthGCPTotal.With(bundleMetricLabels).Add(1)
	}
	if supportedFeatures.Has(featureTLSProfiles) {
		c.SupportsFeatureTLSProfilesTotal.With(bundleMetricLabels).Add(1)
	}
	if hasAllNamespacesModeSupport && !hasOperatorConditionsRBAC && !hasWebhooks && !hasAPIServices && !hasDependencies {
		c.BundleSupportsV1.With(bundleMetricLabels).Add(1)
	}
	if (hasAllNamespacesModeSupport || hasSingleNamespaceModeSupport || hasOwnNamespaceModeSupport) && !hasOperatorConditionsRBAC && !hasAPIServices && !hasDependencies {
		c.BundleSupportsV1TechPreview.With(bundleMetricLabels).Add(1)
	}
	if !hasAllNamespacesModeSupport {
		c.NoAllNamespaceSupport.With(bundleMetricLabels).Add(1)
	}
}
