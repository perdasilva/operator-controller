package convert

import (
    "errors"
    "fmt"

    certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
    certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
    admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
    apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"

    "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

const (
    certManagerInjectCAAnnotation = "cert-manager.io/inject-ca-from"
)

type CertManagerProvider CertificateProvisioningContext

func (p CertManagerProvider) InjectCABundle(obj client.Object) error {
    switch obj.(type) {
    case *admissionregistrationv1.ValidatingWebhookConfiguration:
        p.annotate(obj)
    case *admissionregistrationv1.MutatingWebhookConfiguration:
        p.annotate(obj)
    case *apiextensionsv1.CustomResourceDefinition:
        p.annotate(obj)
    }
    return nil
}

func (p CertManagerProvider) AdditionalObjects() ([]unstructured.Unstructured, error) {
    var (
        objs []unstructured.Unstructured
        errs []error
    )

    issuer := &certmanagerv1.Issuer{
        TypeMeta: metav1.TypeMeta{
            APIVersion: certmanagerv1.SchemeGroupVersion.String(),
            Kind:       "Issuer",
        },
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("%s-selfsigned-issuer", p.CertName),
            Namespace: p.InstallNamespace,
        },
        Spec: certmanagerv1.IssuerSpec{
            IssuerConfig: certmanagerv1.IssuerConfig{
                SelfSigned: &certmanagerv1.SelfSignedIssuer{},
            },
        },
    }
    issuerObj, err := toUnstructured(issuer)
    if err != nil {
        errs = append(errs, err)
    } else {
        objs = append(objs, *issuerObj)
    }

    certificate := &certmanagerv1.Certificate{
        TypeMeta: metav1.TypeMeta{
            APIVersion: certmanagerv1.SchemeGroupVersion.String(),
            Kind:       "Certificate",
        },
        ObjectMeta: metav1.ObjectMeta{
            Name:      p.CertName,
            Namespace: p.InstallNamespace,
        },
        Spec: certmanagerv1.CertificateSpec{
            SecretName: p.CertName,
            Usages:     []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth},
            DNSNames:   []string{fmt.Sprintf("%s.%s.svc", p.WebhookServiceName, p.InstallNamespace)},
            IssuerRef: certmanagermetav1.ObjectReference{
                Name: issuer.GetName(),
            },
        },
    }
    certObj, err := toUnstructured(certificate)
    if err != nil {
        errs = append(errs, err)
    } else {
        objs = append(objs, *certObj)
    }

    if len(errs) > 0 {
        return nil, errors.Join(errs...)
    }
    return objs, nil
}

func (p CertManagerProvider) annotate(obj client.Object) {
    injectionAnnotation := map[string]string{
        certManagerInjectCAAnnotation: fmt.Sprintf("%s/%s", p.InstallNamespace, p.CertName),
    }
    obj.SetAnnotations(util.MergeMaps(injectionAnnotation, obj.GetAnnotations()))
}

func toUnstructured(obj client.Object) (*unstructured.Unstructured, error) {
    gvk := obj.GetObjectKind().GroupVersionKind()

    var u unstructured.Unstructured
    uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
    if err != nil {
        return nil, fmt.Errorf("convert %s %q to unstructured: %w", gvk.Kind, obj.GetName(), err)
    }
    unstructured.RemoveNestedField(uObj, "metadata", "creationTimestamp")
    unstructured.RemoveNestedField(uObj, "status")
    u.Object = uObj
    u.SetGroupVersionKind(gvk)
    return &u, nil
}
