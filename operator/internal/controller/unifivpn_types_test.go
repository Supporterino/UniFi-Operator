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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
)

var _ = Describe("UnifiVpn CRD validation", func() {
	ctx := context.Background()

	It("accepts the two adoption shapes", func() {
		server := &unifiv1alpha1.UnifiVpnServer{
			ObjectMeta: metav1.ObjectMeta{Name: "vpn-accept-server", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiVpnServerSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName}, Name: "Remote Access"},
		}
		tunnel := &unifiv1alpha1.UnifiSiteToSiteTunnel{
			ObjectMeta: metav1.ObjectMeta{Name: "vpn-accept-tunnel", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSiteToSiteTunnelSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName}, Name: testTunnelUpstream},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, server)).To(Succeed())
			Expect(k8sClient.Delete(ctx, tunnel)).To(Succeed())
		})
	})

	It("rejects a VPN server without a name", func() {
		server := &unifiv1alpha1.UnifiVpnServer{
			ObjectMeta: metav1.ObjectMeta{Name: "vpn-server-no-name", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiVpnServerSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteRefName}},
		}
		Expect(k8sClient.Create(ctx, server)).NotTo(Succeed())
	})

	It("rejects a site-to-site tunnel without a siteRef", func() {
		tunnel := &unifiv1alpha1.UnifiSiteToSiteTunnel{
			ObjectMeta: metav1.ObjectMeta{Name: "vpn-tunnel-no-site", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSiteToSiteTunnelSpec{Name: testTunnelUpstream},
		}
		Expect(k8sClient.Create(ctx, tunnel)).NotTo(Succeed())
	})
})

// The envtest drain verifies that a UnifiSite with owned VPN adoption kinds
// initiates their deletion and holds its finalizer until they are gone.
var _ = Describe("UnifiSite drain of VPN resources", func() {
	It("drains owned VPN resources before removing the site finalizer", func() {
		ctx := context.Background()

		site := &unifiv1alpha1.UnifiSite{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "vpn-drain-site",
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

		server := &unifiv1alpha1.UnifiVpnServer{
			ObjectMeta: metav1.ObjectMeta{Name: "vpn-drain-server", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiVpnServerSpec{SiteRef: unifiv1alpha1.CoreRef{Name: site.Name}, Name: "Remote Access"},
		}
		tunnel := &unifiv1alpha1.UnifiSiteToSiteTunnel{
			ObjectMeta: metav1.ObjectMeta{Name: "vpn-drain-tunnel", Namespace: testNamespace},
			Spec:       unifiv1alpha1.UnifiSiteToSiteTunnelSpec{SiteRef: unifiv1alpha1.CoreRef{Name: site.Name}, Name: testTunnelUpstream},
		}
		for _, child := range []client.Object{server, tunnel} {
			Expect(controllerutil.SetControllerReference(site, child, k8sClient.Scheme())).To(Succeed())
			Expect(k8sClient.Create(ctx, child)).To(Succeed())
		}

		reconciler := &UnifiSiteReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: site.Name, Namespace: testNamespace}}

		Expect(k8sClient.Delete(ctx, site)).To(Succeed())

		result, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(childDrainRequeueAfter))

		gotSite := &unifiv1alpha1.UnifiSite{}
		Expect(k8sClient.Get(ctx, request.NamespacedName, gotSite)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer)).To(BeTrue())

		// Adoption kinds carry no finalizer, so the children are already gone.
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: server.Name, Namespace: testNamespace}, &unifiv1alpha1.UnifiVpnServer{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: tunnel.Name, Namespace: testNamespace}, &unifiv1alpha1.UnifiSiteToSiteTunnel{}))).To(BeTrue())

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
