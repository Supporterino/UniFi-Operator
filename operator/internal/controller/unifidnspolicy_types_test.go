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

var _ = Describe("UnifiDnsPolicy CRD validation", func() {
	ctx := context.Background()

	newPolicy := func(name string, spec unifiv1alpha1.UnifiDnsPolicySpec) *unifiv1alpha1.UnifiDnsPolicy {
		spec.SiteRef = unifiv1alpha1.CoreRef{Name: testSiteRefName}
		spec.Domain = "example.com"
		return &unifiv1alpha1.UnifiDnsPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       spec,
		}
	}

	It("accepts a valid A_RECORD policy", func() {
		resource := newPolicy("dns-accept-a", unifiv1alpha1.UnifiDnsPolicySpec{
			Type:    testDnsTypeARecord,
			ARecord: &unifiv1alpha1.DnsARecord{IPv4Address: testIPv4Address, TTLSeconds: 14400},
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("accepts a valid SRV_RECORD policy", func() {
		resource := newPolicy("dns-accept-srv", unifiv1alpha1.UnifiDnsPolicySpec{
			Type: testDnsTypeSrvRecord,
			SrvRecord: &unifiv1alpha1.DnsSrvRecord{
				Port: 389, Priority: 10, Protocol: testSrvProtocol, ServerDomain: testSrvServerDomain, Service: testSrvService, Weight: 5,
			},
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects an A_RECORD carrying SRV fields", func() {
		resource := newPolicy("dns-a-with-srv", unifiv1alpha1.UnifiDnsPolicySpec{
			Type:      testDnsTypeARecord,
			ARecord:   &unifiv1alpha1.DnsARecord{IPv4Address: testIPv4Address, TTLSeconds: 14400},
			SrvRecord: &unifiv1alpha1.DnsSrvRecord{Port: 389, Priority: 10, Protocol: testSrvProtocol, ServerDomain: "s", Service: testSrvService, Weight: 5},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects an SRV_RECORD carrying A fields", func() {
		resource := newPolicy("dns-srv-with-a", unifiv1alpha1.UnifiDnsPolicySpec{
			Type:      testDnsTypeSrvRecord,
			ARecord:   &unifiv1alpha1.DnsARecord{IPv4Address: testIPv4Address, TTLSeconds: 14400},
			SrvRecord: &unifiv1alpha1.DnsSrvRecord{Port: 389, Priority: 10, Protocol: testSrvProtocol, ServerDomain: "s", Service: testSrvService, Weight: 5},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a TXT_RECORD carrying A fields", func() {
		resource := newPolicy("dns-txt-with-a", unifiv1alpha1.UnifiDnsPolicySpec{
			Type:      "TXT_RECORD",
			TxtRecord: &unifiv1alpha1.DnsTxtRecord{Text: "hello"},
			ARecord:   &unifiv1alpha1.DnsARecord{IPv4Address: testIPv4Address, TTLSeconds: 14400},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a variant whose ttlSeconds is out of range", func() {
		resource := newPolicy("dns-a-ttl-range", unifiv1alpha1.UnifiDnsPolicySpec{
			Type:    testDnsTypeARecord,
			ARecord: &unifiv1alpha1.DnsARecord{IPv4Address: testIPv4Address, TTLSeconds: 90000},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})
})

// The envtest drain verifies that a UnifiSite with an owned UnifiDnsPolicy
// initiates its deletion and holds its finalizer until the child is gone.
var _ = Describe("UnifiSite drain of UnifiDnsPolicy", func() {
	It("drains an owned DNS policy before removing the site finalizer", func() {
		ctx := context.Background()

		site := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "dns-drain-site",
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

		policy := &unifiv1alpha1.UnifiDnsPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "dns-drain-policy",
				Namespace:  testNamespace,
				Finalizers: []string{unifiv1alpha1.UnifiDnsPolicyFinalizer},
			},
			Spec: unifiv1alpha1.UnifiDnsPolicySpec{
				SiteRef: unifiv1alpha1.CoreRef{Name: site.Name},
				Type:    testDnsTypeARecord,
				Domain:  "drain.example.com",
				ARecord: &unifiv1alpha1.DnsARecord{IPv4Address: testIPv4Address, TTLSeconds: 14400},
			},
		}
		Expect(controllerutil.SetControllerReference(site, policy, k8sClient.Scheme())).To(Succeed())
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())

		reconciler := &UnifiSiteReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: site.Name, Namespace: testNamespace}}

		Expect(k8sClient.Delete(ctx, site)).To(Succeed())

		result, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(childDrainRequeueAfter))

		gotSite := &unifiv1alpha1.UnifiSite{}
		Expect(k8sClient.Get(ctx, request.NamespacedName, gotSite)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer)).To(BeTrue())

		gotPolicy := &unifiv1alpha1.UnifiDnsPolicy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: policy.Name, Namespace: testNamespace}, gotPolicy)).To(Succeed())
		Expect(gotPolicy.DeletionTimestamp.IsZero()).To(BeFalse())

		gotPolicy.Finalizers = nil
		Expect(k8sClient.Update(ctx, gotPolicy)).To(Succeed())

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
