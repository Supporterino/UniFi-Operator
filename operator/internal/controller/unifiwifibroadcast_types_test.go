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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
)

const (
	testWifiNetworkRef   = "net"
	testWifiSiteRef      = "site"
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
			NetworkRef:                          &unifiv1alpha1.CoreRef{Name: testWifiNetworkRef},
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
			NetworkRef:                          &unifiv1alpha1.CoreRef{Name: testWifiNetworkRef},
			Type:                                "IOT_OPTIMIZED",
			Name:                                testWifiName,
			ClientIsolationEnabled:              false,
			MulticastToUnicastConversionEnabled: true,
			UapsdEnabled:                        true,
			SecurityConfiguration:               openSecurity(),
			IotOptimized:                        &unifiv1alpha1.IotOptimizedWifiOptions{},
		}
	}

	// nativeSpec returns a STANDARD broadcast bound to the identifier-free
	// NATIVE network variant, resolving its site from siteRef rather than
	// transitively through a network.
	nativeSpec := func() unifiv1alpha1.UnifiWifiBroadcastSpec {
		spec := standardSpec()
		spec.NetworkRef = nil
		spec.NativeNetwork = boolPtr(true)
		spec.SiteRef = &unifiv1alpha1.CoreRef{Name: testWifiSiteRef}
		return spec
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

	It("accepts a NATIVE broadcast with a siteRef", func() {
		resource := newBroadcast("wifi-native", nativeSpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects a broadcast with both networkRef and nativeNetwork set", func() {
		spec := standardSpec()
		spec.NativeNetwork = boolPtr(true)
		spec.SiteRef = &unifiv1alpha1.CoreRef{Name: testWifiSiteRef}
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-both-bindings", spec))).NotTo(Succeed())
	})

	It("rejects a broadcast with neither networkRef nor nativeNetwork set", func() {
		spec := standardSpec()
		spec.NetworkRef = nil
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-no-binding", spec))).NotTo(Succeed())
	})

	It("rejects a nativeNetwork:false broadcast with no networkRef", func() {
		// A degenerate binding: nativeNetwork is present but false, and no
		// networkRef is set. The tightened exactly-one rule treats only a
		// present-and-true nativeNetwork as the NATIVE binding, so this must be
		// rejected rather than reaching reconcile with no network.
		spec := standardSpec()
		spec.NetworkRef = nil
		spec.NativeNetwork = boolPtr(false)
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-native-false", spec))).NotTo(Succeed())
	})

	It("rejects a SPECIFIC broadcast that sets siteRef", func() {
		spec := standardSpec()
		spec.SiteRef = &unifiv1alpha1.CoreRef{Name: testWifiSiteRef}
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-specific-site", spec))).NotTo(Succeed())
	})

	It("rejects a NATIVE broadcast without a siteRef", func() {
		spec := nativeSpec()
		spec.SiteRef = nil
		Expect(k8sClient.Create(ctx, newBroadcast("wifi-native-no-site", spec))).NotTo(Succeed())
	})

	It("accepts top-level bandSteeringEnabled for a STANDARD broadcast", func() {
		spec := standardSpec()
		spec.BandSteeringEnabled = boolPtr(true)
		resource := newBroadcast("wifi-standard-band-steering", spec)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("accepts top-level bandSteeringEnabled for an IOT_OPTIMIZED broadcast", func() {
		spec := iotSpec()
		spec.BandSteeringEnabled = boolPtr(false)
		resource := newBroadcast("wifi-iot-band-steering", spec)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects the removed spec.standard.bandSteeringEnabled path", func() {
		typed := newBroadcast("wifi-removed-band-steering", standardSpec())
		object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(typed)
		Expect(err).NotTo(HaveOccurred())
		resource := &unstructured.Unstructured{Object: object}
		resource.SetGroupVersionKind(unifiv1alpha1.GroupVersion.WithKind("UnifiWifiBroadcast"))

		// The typed spec cannot express the removed field, so inject it into the
		// nested standard object to exercise the schema directly.
		Expect(unstructured.SetNestedField(resource.Object, true, "spec", "standard", "bandSteeringEnabled")).To(Succeed())

		// Default field validation only warns and prunes an unknown field, so
		// request strict decoding to prove the old path is gone from the schema.
		err = k8sClient.Create(ctx, resource, &client.CreateOptions{FieldValidation: "Strict"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("bandSteeringEnabled"))
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
		got.Spec.NetworkRef = &unifiv1alpha1.CoreRef{Name: "other-net"}
		Expect(k8sClient.Update(ctx, got)).NotTo(Succeed())
	})

	It("rejects removing the immutable networkRef to convert to NATIVE", func() {
		// A SPECIFIC broadcast must not be converted to NATIVE by unsetting
		// networkRef: the field-level self == oldSelf rule does not fire when a
		// field is set or unset, so a spec-level transition rule rejects the
		// removal. Without it the new object would satisfy the exactly-one and
		// siteRef rules and pass.
		resource := newBroadcast("wifi-immutable-unset", standardSpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		got := &unifiv1alpha1.UnifiWifiBroadcast{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "wifi-immutable-unset", Namespace: testNamespace}, got)).To(Succeed())
		got.Spec.NetworkRef = nil
		got.Spec.NativeNetwork = boolPtr(true)
		got.Spec.SiteRef = &unifiv1alpha1.CoreRef{Name: testWifiSiteRef}

		err := k8sClient.Update(ctx, got)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("network binding cannot be changed"))
	})

	It("rejects adding networkRef to convert a NATIVE broadcast to SPECIFIC", func() {
		// The mirror of the removal case: a NATIVE broadcast must not become
		// SPECIFIC by adding networkRef and clearing nativeNetwork/siteRef.
		// Without the two-directional transition rule the new object would
		// satisfy the exactly-one and siteRef rules and pass, leaving the
		// broadcast owned by a stale parent.
		resource := newBroadcast("wifi-native-to-specific", nativeSpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		got := &unifiv1alpha1.UnifiWifiBroadcast{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "wifi-native-to-specific", Namespace: testNamespace}, got)).To(Succeed())
		got.Spec.NetworkRef = &unifiv1alpha1.CoreRef{Name: testWifiNetworkRef}
		got.Spec.NativeNetwork = nil
		got.Spec.SiteRef = nil

		err := k8sClient.Update(ctx, got)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("network binding cannot be changed"))
	})

	It("rejects re-pointing a NATIVE broadcast's siteRef", func() {
		// A NATIVE broadcast's siteRef determines its ownerReference, so it is
		// immutable: a re-point must be rejected by the field-level self ==
		// oldSelf rule rather than silently swapping the owning site.
		resource := newBroadcast("wifi-native-repoint", nativeSpec())
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		got := &unifiv1alpha1.UnifiWifiBroadcast{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "wifi-native-repoint", Namespace: testNamespace}, got)).To(Succeed())
		got.Spec.SiteRef = &unifiv1alpha1.CoreRef{Name: testOtherSite}

		err := k8sClient.Update(ctx, got)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("siteRef is immutable"))
	})
})
