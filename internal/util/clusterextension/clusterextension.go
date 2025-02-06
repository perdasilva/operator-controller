package clusterextension

import (
	corev1 "k8s.io/api/core/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/features"
)

const (
	clusterExtensionWatchNamespaceAnnotation = "olm.operatorframework.io/watch-namespace"
)

// GetWatchNamespace determines the watch namespace the ClusterExtension should use
// Note: this is a temporary artifice to enable gated use of single/own namespace install modes
// for registry+v1 bundles. This will go away once the ClusterExtension API is updated to include
// (opaque) runtime configuration.
func GetWatchNamespace(ext *ocv1.ClusterExtension) string {
	if features.OperatorControllerFeatureGate.Enabled(features.SingleOwnNamespaceInstallSupport) {
		if ext != nil && ext.Annotations[clusterExtensionWatchNamespaceAnnotation] != "" {
			return ext.Annotations[clusterExtensionWatchNamespaceAnnotation]
		}
	}
	return corev1.NamespaceAll
}
