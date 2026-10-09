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

var _ = Describe("UnifiSite CRD validation", func() {
	ctx := context.Background()

	It("accepts a site with a controllerRef and internalReference", func() {
		resource := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{Name: "site-valid", Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiSiteSpec{
				ControllerRef:     unifiv1alpha1.CoreRef{Name: "controller"},
				InternalReference: "default",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects a site without a controllerRef", func() {
		resource := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{Name: "site-no-controller", Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiSiteSpec{
				InternalReference: "default",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a site without an internalReference", func() {
		resource := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{Name: "site-no-ref", Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiSiteSpec{
				ControllerRef: unifiv1alpha1.CoreRef{Name: "controller"},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})
})
