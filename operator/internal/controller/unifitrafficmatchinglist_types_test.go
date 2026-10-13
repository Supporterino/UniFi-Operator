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

var _ = Describe("UnifiTrafficMatchingList CRD validation", func() {
	ctx := context.Background()

	newList := func(name string, spec unifiv1alpha1.UnifiTrafficMatchingListSpec) *unifiv1alpha1.UnifiTrafficMatchingList {
		return &unifiv1alpha1.UnifiTrafficMatchingList{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       spec,
		}
	}

	It("accepts a valid PORTS list", func() {
		resource := newList("tm-accept-ports", unifiv1alpha1.UnifiTrafficMatchingListSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "Allowed ports",
			Type:    unifiv1alpha1.TrafficMatchingListTypePorts,
			Items: []unifiv1alpha1.TrafficMatchingEntry{
				{Type: testPortEntryNumber, Port: &unifiv1alpha1.TrafficMatchingPortEntry{Value: 80}},
				{Type: testPortEntryRange, PortRange: &unifiv1alpha1.TrafficMatchingPortRangeEntry{Start: 8000, Stop: 8080}},
			},
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("accepts a valid IPV4 list", func() {
		resource := newList("tm-accept-ipv4", unifiv1alpha1.UnifiTrafficMatchingListSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "Protected IPv4",
			Type:    unifiv1alpha1.TrafficMatchingListTypeIPv4,
			Items: []unifiv1alpha1.TrafficMatchingEntry{
				{Type: testEndpointIPAddress, Address: &unifiv1alpha1.TrafficMatchingAddressEntry{Value: "192.168.1.5"}},
				{Type: "SUBNET", Address: &unifiv1alpha1.TrafficMatchingAddressEntry{Value: "192.168.1.0/24"}},
				{Type: "IP_ADDRESS_RANGE", AddressRange: &unifiv1alpha1.TrafficMatchingAddressRangeEntry{Start: testIPv4Address, Stop: "192.168.1.20"}},
			},
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects a PORTS list containing an address entry", func() {
		// A non-numeric/hostname entry under PORTS is modeled as an address
		// entry, which the list CEL rejects.
		resource := newList("tm-ports-with-address", unifiv1alpha1.UnifiTrafficMatchingListSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "Bad ports",
			Type:    unifiv1alpha1.TrafficMatchingListTypePorts,
			Items: []unifiv1alpha1.TrafficMatchingEntry{
				{Type: testEndpointIPAddress, Address: &unifiv1alpha1.TrafficMatchingAddressEntry{Value: "example.com"}},
			},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects an IPV6 list containing an address-range entry", func() {
		resource := newList("tm-ipv6-with-range", unifiv1alpha1.UnifiTrafficMatchingListSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "Bad ipv6",
			Type:    unifiv1alpha1.TrafficMatchingListTypeIPv6,
			Items: []unifiv1alpha1.TrafficMatchingEntry{
				{Type: "IP_ADDRESS_RANGE", AddressRange: &unifiv1alpha1.TrafficMatchingAddressRangeEntry{Start: "fd00::1", Stop: "fd00::9"}},
			},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects an IP_ADDRESS entry without its address value", func() {
		resource := newList("tm-entry-missing-variant", unifiv1alpha1.UnifiTrafficMatchingListSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "Missing variant",
			Type:    unifiv1alpha1.TrafficMatchingListTypeIPv4,
			Items:   []unifiv1alpha1.TrafficMatchingEntry{{Type: testEndpointIPAddress}},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a port number out of range", func() {
		resource := newList("tm-port-out-of-range", unifiv1alpha1.UnifiTrafficMatchingListSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "Out of range",
			Type:    unifiv1alpha1.TrafficMatchingListTypePorts,
			Items: []unifiv1alpha1.TrafficMatchingEntry{
				{Type: testPortEntryNumber, Port: &unifiv1alpha1.TrafficMatchingPortEntry{Value: 70000}},
			},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a list with no items", func() {
		resource := newList("tm-no-items", unifiv1alpha1.UnifiTrafficMatchingListSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "Empty",
			Type:    unifiv1alpha1.TrafficMatchingListTypePorts,
			Items:   []unifiv1alpha1.TrafficMatchingEntry{},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a list without a siteRef", func() {
		resource := newList("tm-no-site", unifiv1alpha1.UnifiTrafficMatchingListSpec{
			Name:  "No site",
			Type:  unifiv1alpha1.TrafficMatchingListTypePorts,
			Items: []unifiv1alpha1.TrafficMatchingEntry{{Type: testPortEntryNumber, Port: &unifiv1alpha1.TrafficMatchingPortEntry{Value: 80}}},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})
})

// The envtest drain verifies that a UnifiSite with an owned
// UnifiTrafficMatchingList initiates the list's deletion and holds its
// finalizer until the list is gone, so the reference chain to the site stays
// resolvable while the child is removed.
var _ = Describe("UnifiSite drain of UnifiTrafficMatchingList", func() {
	It("drains an owned list before removing the site finalizer", func() {
		ctx := context.Background()

		site := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "tm-drain-site",
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

		list := &unifiv1alpha1.UnifiTrafficMatchingList{
			ObjectMeta: metav1.ObjectMeta{Name: "tm-drain-list", Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiTrafficMatchingListSpec{
				SiteRef: unifiv1alpha1.CoreRef{Name: site.Name},
				Name:    "Allowed ports",
				Type:    unifiv1alpha1.TrafficMatchingListTypePorts,
				Items:   []unifiv1alpha1.TrafficMatchingEntry{{Type: testPortEntryNumber, Port: &unifiv1alpha1.TrafficMatchingPortEntry{Value: 80}}},
			},
		}
		Expect(controllerutil.SetControllerReference(site, list, k8sClient.Scheme())).To(Succeed())
		Expect(k8sClient.Create(ctx, list)).To(Succeed())

		reconciler := &UnifiSiteReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: site.Name, Namespace: testNamespace}}

		Expect(k8sClient.Delete(ctx, site)).To(Succeed())

		result, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(childDrainRequeueAfter))

		gotSite := &unifiv1alpha1.UnifiSite{}
		Expect(k8sClient.Get(ctx, request.NamespacedName, gotSite)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer)).To(BeTrue())

		// The list carries no finalizer, so it is already gone.
		err = k8sClient.Get(ctx, types.NamespacedName{Name: list.Name, Namespace: testNamespace}, &unifiv1alpha1.UnifiTrafficMatchingList{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

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
