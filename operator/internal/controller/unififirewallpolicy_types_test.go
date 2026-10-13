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

var _ = Describe("UnifiFirewallPolicy CRD validation", func() {
	ctx := context.Background()

	allow := true

	ipAddress := func() *unifiv1alpha1.FirewallTrafficFilter {
		return &unifiv1alpha1.FirewallTrafficFilter{
			Type:      testEndpointIPAddress,
			IPAddress: &unifiv1alpha1.FirewallTrafficMatchingListFilter{ListRef: unifiv1alpha1.CoreRef{Name: "list"}},
		}
	}

	newPolicy := func(name string, source, destination unifiv1alpha1.FirewallPolicyEndpoint, action unifiv1alpha1.FirewallPolicyAction) *unifiv1alpha1.UnifiFirewallPolicy {
		return &unifiv1alpha1.UnifiFirewallPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiFirewallPolicySpec{
				SiteRef:         unifiv1alpha1.CoreRef{Name: testSiteRefName},
				Name:            "policy",
				Action:          action,
				IPProtocolScope: unifiv1alpha1.FirewallIPProtocolScope{IPVersion: testIPVersionDual},
				Source:          source,
				Destination:     destination,
			},
		}
	}

	configurable := func() unifiv1alpha1.FirewallPolicyEndpoint {
		return unifiv1alpha1.FirewallPolicyEndpoint{ZoneRef: &unifiv1alpha1.CoreRef{Name: "zone"}}
	}
	builtIn := func() unifiv1alpha1.FirewallPolicyEndpoint {
		return unifiv1alpha1.FirewallPolicyEndpoint{BuiltInZone: &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName}}
	}

	It("accepts a valid policy with a configurable source and built-in destination", func() {
		resource := newPolicy("fw-accept", configurable(), builtIn(),
			unifiv1alpha1.FirewallPolicyAction{Type: testActionAllow, AllowReturnTraffic: &allow})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("accepts a source filter referencing a matching list", func() {
		source := configurable()
		source.Filter = ipAddress()
		resource := newPolicy("fw-accept-filter", source, builtIn(),
			unifiv1alpha1.FirewallPolicyAction{Type: testActionBlock})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects an endpoint with neither zoneRef nor builtInZone", func() {
		resource := newPolicy("fw-no-selector", unifiv1alpha1.FirewallPolicyEndpoint{}, builtIn(),
			unifiv1alpha1.FirewallPolicyAction{Type: testActionBlock})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects an endpoint with both zoneRef and builtInZone", func() {
		both := configurable()
		both.BuiltInZone = &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName}
		resource := newPolicy("fw-both-selectors", both, builtIn(),
			unifiv1alpha1.FirewallPolicyAction{Type: testActionBlock})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects ALLOW without allowReturnTraffic", func() {
		resource := newPolicy("fw-allow-no-return", configurable(), builtIn(),
			unifiv1alpha1.FirewallPolicyAction{Type: testActionAllow})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects BLOCK with allowReturnTraffic", func() {
		resource := newPolicy("fw-block-with-return", configurable(), builtIn(),
			unifiv1alpha1.FirewallPolicyAction{Type: testActionBlock, AllowReturnTraffic: &allow})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects an IP_ADDRESS filter without its value", func() {
		source := configurable()
		source.Filter = &unifiv1alpha1.FirewallTrafficFilter{Type: testEndpointIPAddress}
		resource := newPolicy("fw-filter-missing-value", source, builtIn(),
			unifiv1alpha1.FirewallPolicyAction{Type: testActionBlock})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a PORT filter without its value", func() {
		source := configurable()
		source.Filter = &unifiv1alpha1.FirewallTrafficFilter{Type: "PORT"}
		resource := newPolicy("fw-port-missing-value", source, builtIn(),
			unifiv1alpha1.FirewallPolicyAction{Type: testActionBlock})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})
})

var _ = Describe("UnifiFirewallPolicyOrdering CRD validation", func() {
	ctx := context.Background()

	newOrdering := func(name string, source, destination unifiv1alpha1.FirewallZoneSelector) *unifiv1alpha1.UnifiFirewallPolicyOrdering {
		return &unifiv1alpha1.UnifiFirewallPolicyOrdering{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiFirewallPolicyOrderingSpec{
				SiteRef:             unifiv1alpha1.CoreRef{Name: testSiteRefName},
				Source:              source,
				Destination:         destination,
				BeforeSystemDefined: []unifiv1alpha1.CoreRef{{Name: "policy-a"}},
			},
		}
	}

	It("accepts a valid ordering", func() {
		resource := newOrdering("fwo-accept",
			unifiv1alpha1.FirewallZoneSelector{ZoneRef: &unifiv1alpha1.CoreRef{Name: "zone-a"}},
			unifiv1alpha1.FirewallZoneSelector{BuiltInZone: &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName}})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects a zone selector with neither option", func() {
		resource := newOrdering("fwo-no-selector",
			unifiv1alpha1.FirewallZoneSelector{},
			unifiv1alpha1.FirewallZoneSelector{ZoneRef: &unifiv1alpha1.CoreRef{Name: "zone-b"}})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a zone selector with both options", func() {
		resource := newOrdering("fwo-both-selectors",
			unifiv1alpha1.FirewallZoneSelector{
				ZoneRef:     &unifiv1alpha1.CoreRef{Name: "zone-a"},
				BuiltInZone: &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName},
			},
			unifiv1alpha1.FirewallZoneSelector{ZoneRef: &unifiv1alpha1.CoreRef{Name: "zone-b"}})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})
})
