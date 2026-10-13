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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
)

// The envtest drain verifies that a UnifiSite with owned UnifiFirewallPolicy and
// UnifiFirewallPolicyOrdering children initiates their deletion and holds its
// finalizer until they are gone, so the reference chain to the site stays
// resolvable while the children are removed.
var _ = Describe("UnifiSite drain of firewall resources", func() {
	It("drains an owned policy and ordering before removing the site finalizer", func() {
		ctx := context.Background()
		allow := true

		site := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "fw-drain-site",
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

		policy := &unifiv1alpha1.UnifiFirewallPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "fw-drain-policy",
				Namespace:  testNamespace,
				Finalizers: []string{unifiv1alpha1.UnifiFirewallPolicyFinalizer},
			},
			Spec: unifiv1alpha1.UnifiFirewallPolicySpec{
				SiteRef:         unifiv1alpha1.CoreRef{Name: site.Name},
				Name:            "drain policy",
				Action:          unifiv1alpha1.FirewallPolicyAction{Type: testActionAllow, AllowReturnTraffic: &allow},
				IPProtocolScope: unifiv1alpha1.FirewallIPProtocolScope{IPVersion: testTypeIPv4},
				Source:          unifiv1alpha1.FirewallPolicyEndpoint{ZoneRef: &unifiv1alpha1.CoreRef{Name: "src-zone"}},
				Destination:     unifiv1alpha1.FirewallPolicyEndpoint{BuiltInZone: &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName}},
			},
		}
		Expect(controllerutil.SetControllerReference(site, policy, k8sClient.Scheme())).To(Succeed())
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())

		ordering := &unifiv1alpha1.UnifiFirewallPolicyOrdering{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "fw-drain-ordering",
				Namespace:  testNamespace,
				Finalizers: []string{unifiv1alpha1.UnifiFirewallPolicyOrderingFinalizer},
			},
			Spec: unifiv1alpha1.UnifiFirewallPolicyOrderingSpec{
				SiteRef:     unifiv1alpha1.CoreRef{Name: site.Name},
				Source:      unifiv1alpha1.FirewallZoneSelector{ZoneRef: &unifiv1alpha1.CoreRef{Name: "src-zone"}},
				Destination: unifiv1alpha1.FirewallZoneSelector{BuiltInZone: &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName}},
			},
		}
		Expect(controllerutil.SetControllerReference(site, ordering, k8sClient.Scheme())).To(Succeed())
		Expect(k8sClient.Create(ctx, ordering)).To(Succeed())

		reconciler := &UnifiSiteReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: site.Name, Namespace: testNamespace}}

		Expect(k8sClient.Delete(ctx, site)).To(Succeed())

		result, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(childDrainRequeueAfter))

		gotSite := &unifiv1alpha1.UnifiSite{}
		Expect(k8sClient.Get(ctx, request.NamespacedName, gotSite)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer)).To(BeTrue())

		// Both children carry finalizers, so their deletion is initiated and
		// pending: the site finalizer must remain until they finish.
		gotPolicy := &unifiv1alpha1.UnifiFirewallPolicy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: policy.Name, Namespace: testNamespace}, gotPolicy)).To(Succeed())
		Expect(gotPolicy.DeletionTimestamp.IsZero()).To(BeFalse())

		gotOrdering := &unifiv1alpha1.UnifiFirewallPolicyOrdering{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ordering.Name, Namespace: testNamespace}, gotOrdering)).To(Succeed())
		Expect(gotOrdering.DeletionTimestamp.IsZero()).To(BeFalse())

		// Finish both children: clearing the last finalizer on a terminating
		// object removes it (the API server completes the deletion).
		gotPolicy.Finalizers = nil
		Expect(k8sClient.Update(ctx, gotPolicy)).To(Succeed())
		gotOrdering.Finalizers = nil
		Expect(k8sClient.Update(ctx, gotOrdering)).To(Succeed())

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
