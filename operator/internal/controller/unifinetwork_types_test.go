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
	"k8s.io/apimachinery/pkg/types"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
)

const (
	testSiteRefName       = "site"
	testManagementSwitch  = "SWITCH"
	testSwitchNetworkName = "iot"
)

var _ = Describe("UnifiNetwork CRD validation", func() {
	ctx := context.Background()

	newNetwork := func(name string, spec unifiv1alpha1.UnifiNetworkSpec) *unifiv1alpha1.UnifiNetwork {
		return &unifiv1alpha1.UnifiNetwork{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       spec,
		}
	}

	gatewaySpec := func() unifiv1alpha1.UnifiNetworkSpec {
		return unifiv1alpha1.UnifiNetworkSpec{
			SiteRef:    unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Management: "GATEWAY",
			Name:       "lan",
			VLANID:     10,
			Gateway: &unifiv1alpha1.GatewayNetworkOptions{
				CellularBackupEnabled: true,
				InternetAccessEnabled: true,
				IsolationEnabled:      false,
				IPv4Configuration: unifiv1alpha1.GatewayManagedIPv4Configuration{
					AutoScaleEnabled: true,
					HostIPAddress:    testHostIPAddress,
					PrefixLength:     24,
				},
			},
		}
	}

	It("accepts a gateway-managed network", func() {
		resource := newNetwork("net-gateway", gatewaySpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("accepts an unmanaged network", func() {
		resource := newNetwork("net-unmanaged", unifiv1alpha1.UnifiNetworkSpec{
			SiteRef:    unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Management: "UNMANAGED",
			Name:       "guest",
			VLANID:     20,
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("accepts a switch-managed network with a typed device binding", func() {
		resource := newNetwork("net-switch", unifiv1alpha1.UnifiNetworkSpec{
			SiteRef:    unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Management: testManagementSwitch,
			Name:       testSwitchNetworkName,
			VLANID:     30,
			Switch: &unifiv1alpha1.SwitchNetworkOptions{
				CellularBackupEnabled: false,
				IsolationEnabled:      true,
				IPv4Configuration: unifiv1alpha1.SwitchManagedIPv4Configuration{
					AutoScaleEnabled: false,
					HostIPAddress:    "10.0.3.1",
					PrefixLength:     24,
				},
				DeviceTagRef: unifiv1alpha1.CoreRef{Name: "core-switch"},
			},
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("defaults enabled to true", func() {
		resource := newNetwork("net-defaulted", gatewaySpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		got := &unifiv1alpha1.UnifiNetwork{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "net-defaulted", Namespace: testNamespace}, got)).To(Succeed())
		Expect(got.Spec.Enabled).NotTo(BeNil())
		Expect(*got.Spec.Enabled).To(BeTrue())
	})

	It("rejects an unmanaged network that sets gateway fields", func() {
		spec := gatewaySpec()
		spec.Name = "net-mismatch-unmanaged"
		spec.Management = "UNMANAGED"
		Expect(k8sClient.Create(ctx, newNetwork("net-mismatch-unmanaged", spec))).NotTo(Succeed())
	})

	It("rejects a gateway network without gateway fields", func() {
		spec := gatewaySpec()
		spec.Name = "net-mismatch-gateway"
		spec.Gateway = nil
		Expect(k8sClient.Create(ctx, newNetwork("net-mismatch-gateway", spec))).NotTo(Succeed())
	})

	It("rejects a switch network without switch fields", func() {
		Expect(k8sClient.Create(ctx, newNetwork("net-mismatch-switch", unifiv1alpha1.UnifiNetworkSpec{
			SiteRef:    unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Management: testManagementSwitch,
			Name:       testSwitchNetworkName,
			VLANID:     30,
		}))).NotTo(Succeed())
	})

	It("rejects a switch network without a device binding", func() {
		Expect(k8sClient.Create(ctx, newNetwork("net-switch-no-device", unifiv1alpha1.UnifiNetworkSpec{
			SiteRef:    unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Management: testManagementSwitch,
			Name:       testSwitchNetworkName,
			VLANID:     30,
			Switch: &unifiv1alpha1.SwitchNetworkOptions{
				IPv4Configuration: unifiv1alpha1.SwitchManagedIPv4Configuration{
					AutoScaleEnabled: false,
					HostIPAddress:    "10.0.3.1",
					PrefixLength:     24,
				},
			},
		}))).NotTo(Succeed())
	})

	DescribeTable("rejects an out-of-range vlanId",
		func(vlanID int32) {
			spec := gatewaySpec()
			spec.VLANID = vlanID
			Expect(k8sClient.Create(ctx, newNetwork("net-bad-vlan", spec))).NotTo(Succeed())
		},
		Entry("zero", int32(0)),
		Entry("above the maximum", int32(5000)),
	)

	It("rejects a network without a siteRef", func() {
		spec := gatewaySpec()
		spec.SiteRef = unifiv1alpha1.CoreRef{}
		Expect(k8sClient.Create(ctx, newNetwork("net-no-site", spec))).NotTo(Succeed())
	})

	It("rejects a network without a management discriminator", func() {
		spec := gatewaySpec()
		spec.Management = ""
		Expect(k8sClient.Create(ctx, newNetwork("net-no-management", spec))).NotTo(Succeed())
	})

	It("rejects an invalid ipv6 interface type", func() {
		spec := gatewaySpec()
		spec.Gateway.IPv6Configuration = &unifiv1alpha1.IPv6Configuration{
			InterfaceType: "BOGUS",
			ClientAddressAssignment: unifiv1alpha1.IPv6ClientAddressAssignment{
				SLAACEnabled: true,
			},
		}
		Expect(k8sClient.Create(ctx, newNetwork("net-bad-ipv6", spec))).NotTo(Succeed())
	})

	It("rejects a static ipv6 configuration without an address", func() {
		spec := gatewaySpec()
		spec.Gateway.IPv6Configuration = &unifiv1alpha1.IPv6Configuration{
			InterfaceType: "STATIC",
			ClientAddressAssignment: unifiv1alpha1.IPv6ClientAddressAssignment{
				SLAACEnabled: true,
			},
		}
		Expect(k8sClient.Create(ctx, newNetwork("net-bad-ipv6-static", spec))).NotTo(Succeed())
	})
})
