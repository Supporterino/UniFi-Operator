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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// ConnectionFactory builds a UniFi controller client for a console base URL,
// API key, and TLS-verification policy. Production passes unifi.NewClient;
// tests inject a stub so no live console is contacted.
type ConnectionFactory func(baseURL, apiKey string, insecureSkipVerify bool) (unifi.Client, error)

// defaultConnectionFactory adapts unifi.NewClient to ConnectionFactory. The
// concrete *unifi.HTTPClient return cannot be assigned to the interface-typed
// function value directly, so it is wrapped here.
func defaultConnectionFactory(baseURL, apiKey string, insecureSkipVerify bool) (unifi.Client, error) {
	return unifi.NewClient(baseURL, apiKey, insecureSkipVerify)
}

// ResolveError reports that a controller reference or its credential Secret
// could not be resolved. Reason is a Kubernetes condition reason and Message is
// safe to surface in status: neither ever contains credential material. Callers
// detect it with errors.As and fail closed without mutating upstream state.
type ResolveError struct {
	Reason  string
	Message string
}

// Error implements the error interface.
func (e *ResolveError) Error() string { return e.Message }

// readerOrCached returns reader when non-nil, otherwise the cached client. The
// cached client starts a cluster-wide Secret informer the first time it reads a
// Secret, which would force the list/watch RBAC verbs; production therefore
// passes the manager's uncached APIReader. Tests inject a fake client for both
// and leave the reader nil so they fall back here.
func readerOrCached(reader client.Reader, cached client.Client) client.Reader {
	if reader != nil {
		return reader
	}
	return cached
}

// resolveAPIKey reads the API key named by ref from a same-namespace Secret. A
// missing Secret or key produces a *ResolveError with a non-credential reason.
// The returned key must never be logged or written to status
// (docs/security.md); there is deliberately no fallback credential. The reader
// is expected to be uncached so the manager never caches Secrets.
func resolveAPIKey(ctx context.Context, c client.Reader, namespace string, ref *corev1.SecretKeySelector) (string, error) {
	if ref == nil {
		return "", &ResolveError{Reason: reasonSecretNotFound, Message: "spec.secretRef is not set"}
	}
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", &ResolveError{Reason: reasonSecretNotFound, Message: fmt.Sprintf("Secret %q not found", ref.Name)}
		}
		return "", fmt.Errorf("get Secret %q: %w", ref.Name, err)
	}
	value, ok := secret.Data[ref.Key]
	if !ok || len(value) == 0 {
		return "", &ResolveError{Reason: reasonSecretKeyMissing, Message: fmt.Sprintf("Secret %q has no key %q", ref.Name, ref.Key)}
	}
	return string(value), nil
}

// ConnectionResolver resolves a namespaced UnifiController reference and builds
// a UniFi client for it. The site- and network-scoped reconcilers share it so
// the connection and credential resolution live in one place: it reads the
// controller, resolves its same-namespace Secret, and constructs the client
// through the injected factory. The API key is never logged or recorded in
// status.
type ConnectionResolver struct {
	c         client.Client
	reader    client.Reader
	newClient ConnectionFactory
}

// NewConnectionResolver returns a resolver backed by c and newClient. The
// controller is read through the cached client c, while the credential Secret is
// read through reader so the manager's cache never starts a Secret informer. A
// nil reader falls back to c (tests use the fake client for both); a nil
// newClient defaults to unifi.NewClient.
func NewConnectionResolver(c client.Client, reader client.Reader, newClient ConnectionFactory) *ConnectionResolver {
	if newClient == nil {
		newClient = defaultConnectionFactory
	}
	return &ConnectionResolver{c: c, reader: readerOrCached(reader, c), newClient: newClient}
}

// ResolveController reads the UnifiController named by ref in namespace. It
// returns a *ResolveError when the controller is absent or being deleted, and a
// wrapped error for a transient API-server failure. Reconcilers that also need
// the controller's status (for example its detected applicationVersion) call
// this instead of Resolve.
func (r *ConnectionResolver) ResolveController(
	ctx context.Context,
	namespace string,
	ref unifiv1alpha1.CoreRef,
) (*unifiv1alpha1.UnifiController, error) {
	controller := &unifiv1alpha1.UnifiController{}
	if err := r.c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, controller); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &ResolveError{
				Reason:  reasonControllerNotFound,
				Message: fmt.Sprintf("UnifiController %q not found", ref.Name),
			}
		}
		return nil, fmt.Errorf("get UnifiController %q: %w", ref.Name, err)
	}
	if !controller.DeletionTimestamp.IsZero() {
		return nil, &ResolveError{
			Reason:  reasonControllerNotFound,
			Message: fmt.Sprintf("UnifiController %q is being deleted", ref.Name),
		}
	}
	return controller, nil
}

// Resolve returns a UniFi client for the controller named by ref in namespace.
// A missing controller or Secret is a *ResolveError with a non-credential
// reason; other errors are transient and should be retried so
// controller-runtime backs off.
func (r *ConnectionResolver) Resolve(ctx context.Context, namespace string, ref unifiv1alpha1.CoreRef) (unifi.Client, error) {
	controller, err := r.ResolveController(ctx, namespace, ref)
	if err != nil {
		return nil, err
	}
	apiKey, err := resolveAPIKey(ctx, r.reader, namespace, controller.Spec.SecretRef)
	if err != nil {
		return nil, err
	}
	insecure := controller.Spec.InsecureSkipVerify != nil && *controller.Spec.InsecureSkipVerify
	apiClient, err := r.newClient(controller.Spec.URL, apiKey, insecure)
	if err != nil {
		// Client construction rejects an unsafe or malformed base URL. The
		// error text never echoes credentials; this is a terminal spec problem.
		return nil, &ResolveError{Reason: reasonInvalidSpec, Message: err.Error()}
	}
	return apiClient, nil
}

// isCertificateError reports whether err is a TLS certificate verification
// failure. A self-signed or otherwise untrusted console certificate is reported
// as CertificateError rather than a generic connectivity failure, so the only
// way to accept it is the explicit spec.insecureSkipVerify opt-out
// (docs/security.md).
func isCertificateError(err error) bool {
	var verifyErr *tls.CertificateVerificationError
	if errors.As(err, &verifyErr) {
		return true
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return true
	}
	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return true
	}
	var certInvalid x509.CertificateInvalidError
	return errors.As(err, &certInvalid)
}
