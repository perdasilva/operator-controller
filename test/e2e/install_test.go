package e2e

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

const (
	defaultTimeout = 30 * time.Second
	defaultPoll    = 1 * time.Second
)

var _ = Describe("Operator Install", func() {
	When("a valid Operator CR specifying a package", func() {
		var (
			operator *operatorv1alpha1.Operator
			ctx      context.Context
			err      error
		)
		BeforeEach(func() {
			ctx = context.Background()
			operator = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("operator-%s", rand.String(8)),
				},
				Spec: operatorv1alpha1.OperatorSpec{
					PackageName: "prometheus",
				},
			}
		})
		AfterEach(func() {
			By("deleting the testing Operator resource")
			err = c.Delete(ctx, operator)
			Expect(err).ToNot(HaveOccurred())
		})
		It("resolves the specified package with correct bundle path", func() {
			By("creating an operator CR")
			err = c.Create(ctx, operator)
			Expect(err).ToNot(HaveOccurred())
			// TODO dfranz: This test currently relies on the hard-coded CatalogSources found in bundle_cache.go
			// and should be re-worked to use a real or test catalog source when the hard-coded stuff is removed
			By("operator CR status is updated with correct bundle path")
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(operator.Status.BundlePath).To(Equal("operatorhub/prometheus/0.47.0"))
				g.Expect(len(operator.Status.Conditions)).To(Equal(1))
				g.Expect(operator.Status.Conditions[0].Message).To(Equal("resolution was successful"))
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())
		})
	})
})
