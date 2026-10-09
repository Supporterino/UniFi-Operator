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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
)

const testControllerURL = "https://unifi.example.com"

var _ = Describe("UnifiController CRD defaulting and validation", func() {
	ctx := context.Background()

	secretRef := func() *corev1.SecretKeySelector {
		return &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "unifi-api-key"},
			Key:                  "api-key",
		}
	}

	newController := func(name string, spec unifiv1alpha1.UnifiControllerSpec) *unifiv1alpha1.UnifiController {
		return &unifiv1alpha1.UnifiController{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       spec,
		}
	}

	It("defaults insecureSkipVerify to false", func() {
		resource := newController("tls-default", unifiv1alpha1.UnifiControllerSpec{
			URL:       testControllerURL,
			SecretRef: secretRef(),
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		got := &unifiv1alpha1.UnifiController{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tls-default", Namespace: testNamespace}, got)).To(Succeed())
		Expect(got.Spec.InsecureSkipVerify).NotTo(BeNil())
		Expect(*got.Spec.InsecureSkipVerify).To(BeFalse())
	})

	It("accepts an explicit insecureSkipVerify opt-out", func() {
		skip := true
		resource := newController("tls-skip", unifiv1alpha1.UnifiControllerSpec{
			URL:                testControllerURL,
			SecretRef:          secretRef(),
			InsecureSkipVerify: &skip,
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		got := &unifiv1alpha1.UnifiController{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "tls-skip", Namespace: testNamespace}, got)).To(Succeed())
		Expect(got.Spec.InsecureSkipVerify).NotTo(BeNil())
		Expect(*got.Spec.InsecureSkipVerify).To(BeTrue())
	})

	It("rejects a controller without a secretRef", func() {
		resource := newController("no-secret", unifiv1alpha1.UnifiControllerSpec{
			URL: testControllerURL,
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a controller without an absolute https url", func() {
		resource := newController("bad-url", unifiv1alpha1.UnifiControllerSpec{
			URL:       "unifi.example.com",
			SecretRef: secretRef(),
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a cleartext http url", func() {
		resource := newController("cleartext-url", unifiv1alpha1.UnifiControllerSpec{
			URL:       "http://unifi.example.com",
			SecretRef: secretRef(),
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})
})
