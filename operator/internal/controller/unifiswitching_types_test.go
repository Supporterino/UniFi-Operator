/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
)

var _ = Describe("UnifiSwitching CRD validation", func() {
	ctx := context.Background()

	It("accepts the three adoption shapes", func() {
		lag := &unifiv1alpha1.UnifiSwitchingLag{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-accept-lag", Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiSwitchingLagSpec{
				SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
				Type:    testLagTypeLocal,
				Members: []unifiv1alpha1.SwitchingLagMemberSelector{{
					DeviceTag: unifiv1alpha1.DeviceTagSelector{Name: testEnforcingDeviceTag},
					PortIdxs:  []int32{1, 2},
				}},
			},
		}
		domain := &unifiv1alpha1.UnifiSwitchingMcLagDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-accept-domain", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSwitchingMcLagDomainSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName}, Name: "Core MC-LAG"},
		}
		stack := &unifiv1alpha1.UnifiSwitchingSwitchStack{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-accept-stack", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSwitchingSwitchStackSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName}, Name: testSwitchStackName},
		}
		Expect(k8sClient.Create(ctx, lag)).To(Succeed())
		Expect(k8sClient.Create(ctx, domain)).To(Succeed())
		Expect(k8sClient.Create(ctx, stack)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, lag)).To(Succeed())
			Expect(k8sClient.Delete(ctx, domain)).To(Succeed())
			Expect(k8sClient.Delete(ctx, stack)).To(Succeed())
		})
	})

	It("rejects a LAG without a member selector", func() {
		lag := &unifiv1alpha1.UnifiSwitchingLag{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-lag-no-members", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSwitchingLagSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName}, Type: testLagTypeLocal},
		}
		Expect(k8sClient.Create(ctx, lag)).NotTo(Succeed())
	})

	It("rejects a LAG with an invalid type", func() {
		lag := &unifiv1alpha1.UnifiSwitchingLag{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-lag-bad-type", Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiSwitchingLagSpec{
				SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
				Type:    "NOPE",
				Members: []unifiv1alpha1.SwitchingLagMemberSelector{{
					DeviceTag: unifiv1alpha1.DeviceTagSelector{Name: testEnforcingDeviceTag},
					PortIdxs:  []int32{1},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, lag)).NotTo(Succeed())
	})

	It("rejects an MC-LAG domain without a name", func() {
		domain := &unifiv1alpha1.UnifiSwitchingMcLagDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-domain-no-name", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSwitchingMcLagDomainSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName}},
		}
		Expect(k8sClient.Create(ctx, domain)).NotTo(Succeed())
	})

	It("rejects a switch stack without a siteRef", func() {
		stack := &unifiv1alpha1.UnifiSwitchingSwitchStack{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-stack-no-site", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSwitchingSwitchStackSpec{Name: testSwitchStackName},
		}
		Expect(k8sClient.Create(ctx, stack)).NotTo(Succeed())
	})
})

// The envtest drain verifies that a UnifiSite with owned switching adoption kinds
// initiates their deletion and holds its finalizer until they are gone.
var _ = Describe("UnifiSite drain of switching resources", func() {
	It("drains owned switching resources before removing the site finalizer", func() {
		ctx := context.Background()

		site := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "sw-drain-site",
				Namespace:  testNamespace,
				Finalizers: []string{unifiv1alpha1.UnifiSiteFinalizer},
			},
			Spec: unifiv1alpha1.UnifiSiteSpec{
				ControllerRef:     unifiv1alpha1.CoreRef{Name: testControllerName},
				InternalReference: testInternalReference,
			},
		}
		Expect(k8sClient.Create(ctx, site)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, site)
		})

		lag := &unifiv1alpha1.UnifiSwitchingLag{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-drain-lag", Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiSwitchingLagSpec{
				SiteRef: unifiv1alpha1.CoreRef{Name: site.Name},
				Type:    testLagTypeLocal,
				Members: []unifiv1alpha1.SwitchingLagMemberSelector{{
					DeviceTag: unifiv1alpha1.DeviceTagSelector{Name: testEnforcingDeviceTag},
					PortIdxs:  []int32{1},
				}},
			},
		}
		domain := &unifiv1alpha1.UnifiSwitchingMcLagDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-drain-domain", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSwitchingMcLagDomainSpec{SiteRef: unifiv1alpha1.CoreRef{Name: site.Name}, Name: "Core MC-LAG"},
		}
		stack := &unifiv1alpha1.UnifiSwitchingSwitchStack{
			ObjectMeta: metav1.ObjectMeta{Name: "sw-drain-stack", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSwitchingSwitchStackSpec{SiteRef: unifiv1alpha1.CoreRef{Name: site.Name}, Name: testSwitchStackName},
		}
		for _, child := range []client.Object{lag, domain, stack} {
			Expect(controllerutil.SetControllerReference(site, child, k8sClient.Scheme())).To(Succeed())
			Expect(k8sClient.Create(ctx, child)).To(Succeed())
		}

		reconciler := &UnifiSiteReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: site.Name, Namespace: testNamespace}}

		Expect(k8sClient.Delete(ctx, site)).To(Succeed())

		result, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(childDrainRequeueAfter))

		gotSite := &unifiv1alpha1.UnifiSite{}
		Expect(k8sClient.Get(ctx, request.NamespacedName, gotSite)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer)).To(BeTrue())

		// Adoption kinds carry no finalizer, so the children are already gone.
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: lag.Name, Namespace: testNamespace}, &unifiv1alpha1.UnifiSwitchingLag{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: domain.Name, Namespace: testNamespace}, &unifiv1alpha1.UnifiSwitchingMcLagDomain{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: stack.Name, Namespace: testNamespace}, &unifiv1alpha1.UnifiSwitchingSwitchStack{}))).To(BeTrue())

		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		gotSite = &unifiv1alpha1.UnifiSite{}
		err = k8sClient.Get(ctx, request.NamespacedName, gotSite)
		if err == nil {
			Expect(controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer)).To(BeFalse())
		} else {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}
	})
})
