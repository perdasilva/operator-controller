package convert

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CertificateProvisioningContext struct {
	WebhookServiceName string
	CertName           string
	InstallNamespace   string
}

type CertificateProvider interface {
	InjectCABundle(obj client.Object) error
	AdditionalObjects() ([]unstructured.Unstructured, error)
}

type CertSecretInfo struct {
	SecretName     string
	CertificateKey string
	PrivateKeyKey  string
}
