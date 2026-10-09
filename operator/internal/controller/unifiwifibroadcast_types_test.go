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
	testWifiNetworkRef   = "net"
	testWifiName         = "corp"
	testWifiWPA2Personal = "WPA2_PERSONAL"
)

var _ = Describe("UnifiWifiBroadcast CRD validation", func() {
	ctx := context.Background()

	newBroadcast := func(name string, spec unifiv1alpha1.UnifiWifiBroadcastSpec) *unifiv1alpha1.UnifiWifiBroadcast {
		return &unifiv1alpha1.UnifiWifiBroadcast{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       spec,
		}
	}

	openSecurity := func() unifiv1alpha1.WifiSecurityConfiguration {
		return unifiv1alpha1.WifiSecurityConfiguration{
			Type: testWifiSecurityOpen,
			Open: &unifiv1alpha1.WifiOpenSecurityConfiguration{},
		}
	}

	standardSpec := func() unifiv1alpha1.UnifiWifiBroadcastSpec {
		return unifiv1alpha1.UnifiWifiBroadcastSpec{
			NetworkRef:                          unifiv1alpha1.CoreRef{Name: testWifiNetworkRef},
			Type:                                "STANDARD",
			Name:                                testWifiName,
			ClientIsolationEnabled:              false,
			MulticastToUnicastConversionEnabled: true,
			UapsdEnabled:                        false,
			SecurityConfiguration:               openSecurity(),
			Standard: &unifiv1alpha1.StandardWifiOptions{
				BroadcastingFrequenciesGHz: []unifiv1alpha1.WifiBroadcastingFrequencyGHz{"2.4", "5"},
			},
		}
	}

	iotSpec := func() unifiv1alpha1.UnifiWifiBroadcastSpec {
		return unifiv1alpha1.UnifiWifiBroadcastSpec{
			NetworkRef:                          unifiv1alpha1.CoreRef{Name: testWifiNetworkRef},
			Type:                                "IOT_OPTIMIZED",
			Name:                                testWifiName,
			ClientIsolationEnabled:              false,
			MulticastToUnicastConversionEnabled: true,
			UapsdEnabled:                        true,
			SecurityConfiguration:               openSecurity(),
			IotOptimized:                        &unifiv1alpha1.IotOptimizedWifiOptions{},
		}
	}

	It("accepts a STANDARD broadcast", func() {
		resource := newBroadcast("wifi-standard", standardSpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("accepts an IOT_OPTIMIZED broadcast", func() {
		resource := newBroadcast("wifi-iot", iotSpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("defaults the common broadcast fields", func() {
		resource := newBroadcast("wifi-defaulted", standardSpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		got := &unifiv1alpha1.UnifiWifiBroadcast{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "wifi-defaulted", Namespace: testNamespace}, got)).To(Succeed())
		Expect(got.Spec.Enabled).NotTo(BeNil())
		Expect(*got.Spec.Enabled).To(BeTrue())
		Expect(got.Spec.Channel2gLockedTo6).NotTo(BeNil())
		Expect(*got.Spec.Channel2gLockedTo6).To(BeFalse())
		Expect(got.Spec.DtimPeriod2gLockedTo3).NotTo(BeNil())
		Expect(*got.Spec.DtimPeriod2gLockedTo3).To(BeFalse())
	})

	It("rejects a STANDARD broadcast without the standard variant", func() {
		spec := standardSpec()
		spec.Standard = nil
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-standard-missing", spec))).NotTo(Succeed())
	})

	It("rejects an IOT_OPTIMIZED broadcast that sets standard fields", func() {
		spec := iotSpec()
		spec.Standard = &unifiv1alpha1.StandardWifiOptions{
			BroadcastingFrequenciesGHz: []unifiv1alpha1.WifiBroadcastingFrequencyGHz{"5"},
		}
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-iot-mismatch", spec))).NotTo(Succeed())
	})

	It("rejects a security type mismatch", func() {
		spec := standardSpec()
		spec.SecurityConfiguration = unifiv1alpha1.WifiSecurityConfiguration{Type: testWifiWPA2Personal}
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-security-mismatch", spec))).NotTo(Succeed())
	})

	It("rejects an empty device-tag selector", func() {
		spec := standardSpec()
		spec.DeviceTags = []unifiv1alpha1.DeviceTagSelector{{Name: ""}}
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-empty-tag", spec))).NotTo(Succeed())
	})

	DescribeTable("rejects an out-of-range group rekey interval",
		func(seconds int32) {
			spec := standardSpec()
			spec.SecurityConfiguration = unifiv1alpha1.WifiSecurityConfiguration{
				Type:         testWifiWPA2Personal,
				WPA2Personal: &unifiv1alpha1.WifiWPA2PersonalSecurityConfiguration{GroupRekeyIntervalSeconds: &seconds},
			}
			Expect(k8sClient.Create(ctx, newBroadcast("wifi-bad-rekey", spec))).NotTo(Succeed())
		},
		Entry("zero", int32(0)),
		Entry("above the maximum", int32(86401)),
	)

	DescribeTable("rejects an out-of-range DTIM override",
		func(period int32) {
			spec := standardSpec()
			spec.Standard.DtimPeriodByFrequencyGHzOverride = &unifiv1alpha1.WifiDtimPeriodConfiguration{
				GHz2_4: period,
				GHz5:   2,
				GHz6:   2,
			}
			Expect(k8sClient.Create(ctx, newBroadcast("wifi-bad-dtim", spec))).NotTo(Succeed())
		},
		Entry("zero", int32(0)),
		Entry("above the maximum", int32(256)),
	)

	It("rejects an invalid basic data rate", func() {
		spec := standardSpec()
		spec.BasicDataRateKbpsByFrequencyGHz = &unifiv1alpha1.WifiBasicDataRateConfiguration{
			GHz2_4: 1234,
			GHz5:   6000,
		}
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-bad-rate", spec))).NotTo(Succeed())
	})

	It("rejects an invalid RADIUS MAC address format", func() {
		spec := standardSpec()
		spec.SecurityConfiguration = unifiv1alpha1.WifiSecurityConfiguration{
			Type: testWifiWPA2Personal,
			WPA2Personal: &unifiv1alpha1.WifiWPA2PersonalSecurityConfiguration{
				RadiusConfiguration: &unifiv1alpha1.WifiNonEnterpriseRadiusConfiguration{
					MACAuthenticationConfiguration: unifiv1alpha1.WifiRadiusMacAuthenticationConfiguration{
						MacAddressFormat: "BOGUS",
					},
					NasID:            unifiv1alpha1.WifiRadiusNasID{Type: testNasIDUserDefined, Value: testNasIDValue},
					RadiusProfileRef: unifiv1alpha1.CoreRef{Name: "profile"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-bad-mac", spec))).NotTo(Succeed())
	})

	It("rejects an unsupported broadcasting frequency", func() {
		spec := standardSpec()
		spec.Standard.BroadcastingFrequenciesGHz = []unifiv1alpha1.WifiBroadcastingFrequencyGHz{"7"}
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-bad-freq", spec))).NotTo(Succeed())
	})

	It("rejects a change to the immutable networkRef", func() {
		resource := newBroadcast("wifi-immutable-ref", standardSpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		got := &unifiv1alpha1.UnifiWifiBroadcast{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "wifi-immutable-ref", Namespace: testNamespace}, got)).To(Succeed())
		got.Spec.NetworkRef = unifiv1alpha1.CoreRef{Name: "other-net"}
		Expect(k8sClient.Update(ctx, got)).NotTo(Succeed())
	})
})
