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

var _ = Describe("UnifiAclRule CRD validation", func() {
	ctx := context.Background()

	ipv4Source := func() unifiv1alpha1.AclRuleEndpoint {
		return unifiv1alpha1.AclRuleEndpoint{
			Type:     testEndpointNetworks,
			Networks: &unifiv1alpha1.AclNetworksEndpoint{NetworkRefs: []unifiv1alpha1.CoreRef{{Name: "net"}}},
		}
	}
	ipv4Destination := func() unifiv1alpha1.AclRuleEndpoint {
		return unifiv1alpha1.AclRuleEndpoint{Type: testEndpointPorts, Ports: &unifiv1alpha1.AclPortsEndpoint{Ports: []unifiv1alpha1.AclPort{80, 443}}}
	}
	macEndpoint := func() unifiv1alpha1.AclRuleEndpoint {
		return unifiv1alpha1.AclRuleEndpoint{
			Type:         "MAC_ADDRESSES",
			MacAddresses: &unifiv1alpha1.AclMacAddressesEndpoint{MacAddresses: []string{"aa:bb:cc:dd:ee:ff"}},
		}
	}
	networkRef := func() *unifiv1alpha1.CoreRef {
		return &unifiv1alpha1.CoreRef{Name: "net"}
	}

	newRule := func(name string, spec unifiv1alpha1.UnifiAclRuleSpec) *unifiv1alpha1.UnifiAclRule {
		spec.SiteRef = unifiv1alpha1.CoreRef{Name: testSiteRefName}
		spec.Name = name
		return &unifiv1alpha1.UnifiAclRule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       spec,
		}
	}

	It("accepts a valid IPV4 rule", func() {
		resource := newRule("acl-accept-ipv4", unifiv1alpha1.UnifiAclRuleSpec{
			Action:      testActionBlock,
			Type:        testTypeIPv4,
			Source:      ipv4Source(),
			Destination: ipv4Destination(),
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("accepts a valid MAC rule", func() {
		resource := newRule("acl-accept-mac", unifiv1alpha1.UnifiAclRuleSpec{
			Action:      testActionAllow,
			Type:        testTypeMac,
			Source:      macEndpoint(),
			Destination: macEndpoint(),
			NetworkRef:  networkRef(),
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects MAC filters under type IPV4", func() {
		resource := newRule("acl-mismatch-ipv4", unifiv1alpha1.UnifiAclRuleSpec{
			Action:      testActionBlock,
			Type:        testTypeIPv4,
			Source:      macEndpoint(),
			Destination: ipv4Destination(),
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects IP-address filters under type MAC", func() {
		resource := newRule("acl-mismatch-mac", unifiv1alpha1.UnifiAclRuleSpec{
			Action:      testActionAllow,
			Type:        testTypeMac,
			Source:      ipv4Source(),
			Destination: macEndpoint(),
			NetworkRef:  networkRef(),
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a MAC rule without a networkRef", func() {
		resource := newRule("acl-mac-no-network", unifiv1alpha1.UnifiAclRuleSpec{
			Action:      testActionAllow,
			Type:        testTypeMac,
			Source:      macEndpoint(),
			Destination: macEndpoint(),
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects an IPV4 rule with a networkRef", func() {
		resource := newRule("acl-ipv4-with-network", unifiv1alpha1.UnifiAclRuleSpec{
			Action:      testActionBlock,
			Type:        testTypeIPv4,
			Source:      ipv4Source(),
			Destination: ipv4Destination(),
			NetworkRef:  networkRef(),
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects an endpoint with a missing variant value", func() {
		resource := newRule("acl-missing-variant", unifiv1alpha1.UnifiAclRuleSpec{
			Action:      testActionBlock,
			Type:        testTypeIPv4,
			Source:      unifiv1alpha1.AclRuleEndpoint{Type: testEndpointPorts},
			Destination: ipv4Destination(),
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})
})

// The envtest drain verifies that a UnifiSite with owned UnifiAclRule and
// UnifiAclRuleOrdering children initiates their deletion and holds its finalizer
// until they are gone.
var _ = Describe("UnifiSite drain of ACL resources", func() {
	It("drains an owned ACL rule and ordering before removing the site finalizer", func() {
		ctx := context.Background()

		site := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "acl-drain-site",
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

		rule := &unifiv1alpha1.UnifiAclRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "acl-drain-rule",
				Namespace:  testNamespace,
				Finalizers: []string{unifiv1alpha1.UnifiAclRuleFinalizer},
			},
			Spec: unifiv1alpha1.UnifiAclRuleSpec{
				SiteRef:     unifiv1alpha1.CoreRef{Name: site.Name},
				Name:        "drain rule",
				Action:      testActionBlock,
				Type:        testTypeIPv4,
				Source:      unifiv1alpha1.AclRuleEndpoint{Type: testEndpointPorts, Ports: &unifiv1alpha1.AclPortsEndpoint{Ports: []unifiv1alpha1.AclPort{80}}},
				Destination: unifiv1alpha1.AclRuleEndpoint{Type: testEndpointPorts, Ports: &unifiv1alpha1.AclPortsEndpoint{Ports: []unifiv1alpha1.AclPort{443}}},
			},
		}
		Expect(controllerutil.SetControllerReference(site, rule, k8sClient.Scheme())).To(Succeed())
		Expect(k8sClient.Create(ctx, rule)).To(Succeed())

		ordering := &unifiv1alpha1.UnifiAclRuleOrdering{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "acl-drain-ordering",
				Namespace:  testNamespace,
				Finalizers: []string{unifiv1alpha1.UnifiAclRuleOrderingFinalizer},
			},
			Spec: unifiv1alpha1.UnifiAclRuleOrderingSpec{
				SiteRef:         unifiv1alpha1.CoreRef{Name: site.Name},
				OrderedAclRules: []unifiv1alpha1.CoreRef{{Name: rule.Name}},
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

		gotRule := &unifiv1alpha1.UnifiAclRule{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rule.Name, Namespace: testNamespace}, gotRule)).To(Succeed())
		Expect(gotRule.DeletionTimestamp.IsZero()).To(BeFalse())

		gotOrdering := &unifiv1alpha1.UnifiAclRuleOrdering{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ordering.Name, Namespace: testNamespace}, gotOrdering)).To(Succeed())
		Expect(gotOrdering.DeletionTimestamp.IsZero()).To(BeFalse())

		gotRule.Finalizers = nil
		Expect(k8sClient.Update(ctx, gotRule)).To(Succeed())
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
