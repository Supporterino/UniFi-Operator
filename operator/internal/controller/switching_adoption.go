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
	"errors"
	"fmt"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// adoptionSite is the outcome of resolving an adopted child's parent site. It is
// shared by the read-only adoption kinds (RADIUS, switching, VPN) so their
// site-resolution semantics stay identical.
type adoptionSite struct {
	site    *unifiv1alpha1.UnifiSite
	reason  string
	message string
	retry   bool
	err     error
}

// failed reports whether the resolution failed and the caller must stop.
func (a adoptionSite) failed() bool { return a.reason != "" || a.err != nil }

// resolveAdoptionSite resolves a name-only siteRef for an adoption kind, failing
// closed when the site is absent, deleting, or has not yet adopted an upstream
// site.
func resolveAdoptionSite(ctx context.Context, reader client.Reader, namespace, siteName string) adoptionSite {
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: namespace, Name: siteName}
	if err := reader.Get(ctx, key, site); err != nil {
		if apierrors.IsNotFound(err) {
			return adoptionSite{reason: reasonSiteRefNotFound, message: fmt.Sprintf("UnifiSite %q not found", siteName), retry: true}
		}
		return adoptionSite{err: fmt.Errorf("get UnifiSite %q: %w", siteName, err)}
	}
	if !site.DeletionTimestamp.IsZero() {
		return adoptionSite{reason: reasonSiteRefNotFound, message: fmt.Sprintf("UnifiSite %q is being deleted", site.Name), retry: true}
	}
	if site.Status.SiteID == "" {
		return adoptionSite{reason: reasonSiteNotAdopted, message: fmt.Sprintf("UnifiSite %q has not adopted an upstream site yet (status.siteID is empty)", site.Name), retry: true}
	}
	return adoptionSite{site: site}
}

// classifyUpstreamError maps a failed upstream read onto the terminal status
// (reason, message) a reconciler should report, or retry=true when the failure
// is transient and should be returned for controller-runtime backoff. It is
// shared by the adoption-kind reconcilers.
func classifyUpstreamError(err error) (reason, message string, retry bool) {
	if isCertificateError(err) {
		return reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", false
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return reasonAuthenticationFailed, "console rejected the API key; verify the controller Secret", false
		case !apiErr.Retryable():
			return reasonInvalidSpec, "console rejected the request: " + apiErr.Error(), false
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return reasonInvalidSpec, specErr.Error(), false
	}
	return "", "", true
}
