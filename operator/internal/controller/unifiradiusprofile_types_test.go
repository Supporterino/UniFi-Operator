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

var _ = Describe("UnifiRadiusProfile CRD validation", func() {
	ctx := context.Background()

	newProfile := func(name string, spec unifiv1alpha1.UnifiRadiusProfileSpec) *unifiv1alpha1.UnifiRadiusProfile {
		return &unifiv1alpha1.UnifiRadiusProfile{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       spec,
		}
	}

	It("accepts an adoption spec with a siteRef and a name", func() {
		resource := newProfile("radius-accept", unifiv1alpha1.UnifiRadiusProfileSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    testRadiusProfileUpstreamName,
		})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
	})

	It("rejects a profile without a siteRef", func() {
		resource := newProfile("radius-no-site", unifiv1alpha1.UnifiRadiusProfileSpec{
			Name: testRadiusProfileUpstreamName,
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects a profile with an empty name", func() {
		resource := newProfile("radius-empty-name", unifiv1alpha1.UnifiRadiusProfileSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName},
			Name:    "",
		})
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})
})

// The envtest drain verifies that a UnifiSite with an owned UnifiRadiusProfile
// initiates the profile's deletion and holds its finalizer until the profile is
// gone, so the reference chain to the site stays resolvable while the child is
// removed. Adoption carries no finalizer, so the profile is deleted directly.
var _ = Describe("UnifiSite drain of UnifiRadiusProfile", func() {
	It("drains an owned profile before removing the site finalizer", func() {
		ctx := context.Background()

		site := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "radius-drain-site",
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

		profile := &unifiv1alpha1.UnifiRadiusProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "radius-drain-profile", Namespace: testNamespace},
			Spec: unifiv1alpha1.UnifiRadiusProfileSpec{
				SiteRef: unifiv1alpha1.CoreRef{Name: site.Name},
				Name:    testRadiusProfileUpstreamName,
			},
		}
		Expect(controllerutil.SetControllerReference(site, profile, k8sClient.Scheme())).To(Succeed())
		Expect(k8sClient.Create(ctx, profile)).To(Succeed())

		reconciler := &UnifiSiteReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: site.Name, Namespace: testNamespace}}

		Expect(k8sClient.Delete(ctx, site)).To(Succeed())

		// First reconcile initiates the profile's deletion and holds the site
		// finalizer because the drain is not yet complete on this pass.
		result, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(childDrainRequeueAfter))

		gotSite := &unifiv1alpha1.UnifiSite{}
		Expect(k8sClient.Get(ctx, request.NamespacedName, gotSite)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer)).To(BeTrue())

		// The profile carries no finalizer, so it is already gone.
		err = k8sClient.Get(ctx, types.NamespacedName{Name: profile.Name, Namespace: testNamespace}, &unifiv1alpha1.UnifiRadiusProfile{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		// Second reconcile observes no owned children and removes the finalizer.
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
