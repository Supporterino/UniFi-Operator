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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	readyCondition    = "Ready"
	testNamespace     = "default"
	testSite          = "default"
	testNetwork       = "lan"
	testUpstreamID    = "upstream-1"
	testResourceName  = "net"
	statusSubresource = "status"
)

var _ = Describe("UnifiNetwork Controller", func() {
	ctx := context.Background()

	reconcilerFor := func(networks ...unifi.Network) *UnifiNetworkReconciler {
		return &UnifiNetworkReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			UniFi:  unifi.NewFixtureClient(networks...),
		}
	}

	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: testNamespace}

		It("applies the CRD and reports a Ready condition with observedGeneration", func() {
			resource := &unifiv1alpha1.UnifiNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: testNamespace},
				Spec:       unifiv1alpha1.UnifiNetworkSpec{Site: testSite, Name: testNetwork},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			})

			_, err := reconcilerFor(unifi.Network{ID: testUpstreamID, Name: testNetwork, Enabled: true}).
				Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			got := &unifiv1alpha1.UnifiNetwork{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, got)).To(Succeed())

			cond := apimeta.FindStatusCondition(got.Status.Conditions, readyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
			Expect(got.Status.ControllerID).To(Equal(testUpstreamID))
		})
	})

	Context("When the spec is defaulted", func() {
		It("defaults enabled to true", func() {
			resource := &unifiv1alpha1.UnifiNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "defaulted", Namespace: testNamespace},
				Spec:       unifiv1alpha1.UnifiNetworkSpec{Site: testSite, Name: testNetwork},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			})

			got := &unifiv1alpha1.UnifiNetwork{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "defaulted", Namespace: testNamespace}, got)).To(Succeed())
			Expect(got.Spec.Enabled).NotTo(BeNil())
			Expect(*got.Spec.Enabled).To(BeTrue())
		})
	})

	Context("When the spec is invalid", func() {
		It("rejects a network without a name or site", func() {
			resource := &unifiv1alpha1.UnifiNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "missing-fields", Namespace: testNamespace},
				Spec:       unifiv1alpha1.UnifiNetworkSpec{},
			}
			Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
		})

		It("rejects an out-of-range VLAN", func() {
			resource := &unifiv1alpha1.UnifiNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "bad-vlan", Namespace: testNamespace},
				Spec: unifiv1alpha1.UnifiNetworkSpec{
					Site: testSite,
					Name: testNetwork,
					VLAN: ptrInt32(5000),
				},
			}
			Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
		})

		It("rejects a malformed subnet", func() {
			resource := &unifiv1alpha1.UnifiNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "bad-subnet", Namespace: testNamespace},
				Spec: unifiv1alpha1.UnifiNetworkSpec{
					Site:   testSite,
					Name:   testNetwork,
					Subnet: "not-a-cidr",
				},
			}
			Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
		})
	})
})

func ptrInt32(v int32) *int32 { return &v }
