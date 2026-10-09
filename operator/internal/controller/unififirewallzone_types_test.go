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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
)

var _ = Describe("UnifiFirewallZone CRD validation", func() {
	ctx := context.Background()

	newZone := func(name string, spec unifiv1alpha1.UnifiFirewallZoneSpec) *unifiv1alpha1.UnifiFirewallZone {
		return &unifiv1alpha1.UnifiFirewallZone{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       spec,
		}
	}

	It("accepts a zone with no networkRefs", func() {
		resource := newZone("zone-empty-members", unifiv1alpha1.UnifiFirewallZoneSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "Internal",
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("accepts a zone with networkRefs", func() {
		resource := newZone("zone-with-members", unifiv1alpha1.UnifiFirewallZoneSpec{
			SiteRef:     unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:        "IoT",
			NetworkRefs: []unifiv1alpha1.CoreRef{{Name: "net-gateway"}, {Name: "net-unmanaged"}},
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects a zone without a siteRef", func() {
		resource := newZone("zone-no-site", unifiv1alpha1.UnifiFirewallZoneSpec{
			Name: "Internal",
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a zone without a name", func() {
		resource := newZone("zone-no-name", unifiv1alpha1.UnifiFirewallZoneSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a zone with an empty name", func() {
		resource := newZone("zone-empty-name", unifiv1alpha1.UnifiFirewallZoneSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "",
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a networkRef without a name", func() {
		resource := newZone("zone-empty-member", unifiv1alpha1.UnifiFirewallZoneSpec{
			SiteRef:     unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:        "IoT",
			NetworkRefs: []unifiv1alpha1.CoreRef{{Name: ""}},
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})
})
