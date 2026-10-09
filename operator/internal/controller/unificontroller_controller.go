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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// credentialResolutionRequeueAfter bounds how long the controller waits before
// retrying a UnifiController whose Secret is not yet present. A Secret creation
// does not change the controller's generation, so the periodic requeue is what
// lets a late-appearing Secret self-heal.
const credentialResolutionRequeueAfter = 30 * time.Second

// UnifiControllerReconciler reconciles a UnifiController object. It resolves the
// console connection (URL, API key from a Secret, TLS policy), probes
// GET /v1/info, records the detected application version, and gates the
// Integration v1 surface on the pinned minimum version. The API key is never
// logged or written to status.
type UnifiControllerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIReader reads credential Secrets directly from the API server, bypassing
	// the manager cache, so no cluster-wide Secret informer is started and the
	// RBAC needs only the "get" verb. It falls back to the cached client when
	// nil (tests use the fake client for both).
	APIReader client.Reader

	// NewClient constructs the UniFi client. It defaults to unifi.NewClient and
	// is injectable so tests substitute a stub.
	NewClient ConnectionFactory
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile resolves the controller's Secret, probes the console, and reports
// readiness and the detected application version in status. It is idempotent:
// re-running it on an unchanged object performs no writes. It fails closed on a
// missing Secret, an untrusted certificate, an auth rejection, or an
// unsupported application version and never falls back to a default credential.
func (r *UnifiControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	controller := &unifiv1alpha1.UnifiController{}
	if err := r.Get(ctx, req.NamespacedName, controller); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	apiKey, err := resolveAPIKey(ctx, readerOrCached(r.APIReader, r.Client), controller.Namespace, controller.Spec.SecretRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			log.Info("controller credentials unresolved", "reason", resolveErr.Reason)
			return r.requeueStatus(ctx, controller, false, resolveErr.Reason, resolveErr.Message, "")
		}
		return ctrl.Result{}, fmt.Errorf("resolve API key: %w", err)
	}

	newClient := r.NewClient
	if newClient == nil {
		newClient = defaultConnectionFactory
	}
	insecure := controller.Spec.InsecureSkipVerify != nil && *controller.Spec.InsecureSkipVerify
	apiClient, err := newClient(controller.Spec.URL, apiKey, insecure)
	if err != nil {
		// An unsafe or malformed base URL is a spec problem, not a credential
		// one, and the error text is built without echoing credentials.
		return r.updateStatus(ctx, controller, false, reasonInvalidSpec, err.Error(), "")
	}

	info, err := apiClient.GetInfo(ctx)
	if err != nil {
		return r.handleInfoError(ctx, controller, err)
	}

	if err := CheckCapability(info.ApplicationVersion, CapabilityOfficialAPI); err != nil {
		return r.updateStatus(ctx, controller, false, reasonVersionUnsupported, err.Error(), info.ApplicationVersion)
	}

	summary := fmt.Sprintf("connected to %s; application version %s", controller.Spec.URL, info.ApplicationVersion)
	return r.updateStatus(ctx, controller, true, reasonReconciled, summary, info.ApplicationVersion)
}

// handleInfoError classifies a failed reachability probe. Certificate and auth
// failures are terminal and fail closed; a transient failure is returned so
// controller-runtime backs off.
func (r *UnifiControllerReconciler) handleInfoError(
	ctx context.Context,
	controller *unifiv1alpha1.UnifiController,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, controller, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", "")
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, controller, false, reasonAuthenticationFailed,
				"console rejected the API key; verify spec.secretRef", "")
		case !apiErr.Retryable():
			return r.updateStatus(ctx, controller, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), "")
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, controller, false, reasonInvalidSpec, specErr.Error(), "")
	}
	log.Error(err, "transient error probing console")
	return ctrl.Result{}, fmt.Errorf("get application info: %w", err)
}

// requeueStatus writes status and re-checks after a bounded delay. It is used
// when resolution failed for a reason that a later edit (for example creating
// the Secret) can fix but that does not change this object's generation.
func (r *UnifiControllerReconciler) requeueStatus(
	ctx context.Context,
	controller *unifiv1alpha1.UnifiController,
	ready bool,
	reason, message, applicationVersion string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, controller, ready, reason, message, applicationVersion)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = credentialResolutionRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op.
func (r *UnifiControllerReconciler) updateStatus(
	ctx context.Context,
	controller *unifiv1alpha1.UnifiController,
	ready bool,
	reason, message, applicationVersion string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if controller.Status.ObservedGeneration != controller.Generation {
		controller.Status.ObservedGeneration = controller.Generation
		changed = true
	}
	if controller.Status.Summary != message {
		controller.Status.Summary = message
		changed = true
	}
	if controller.Status.ApplicationVersion != applicationVersion {
		controller.Status.ApplicationVersion = applicationVersion
		changed = true
	}
	if setReadyCondition(&controller.Status.Conditions, controller.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, controller); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager. Status-only writes
// do not change the object's generation, so the predicate drops the resulting
// no-op reconcile while still handling spec changes.
func (r *UnifiControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiController{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("unificontroller").
		Complete(r)
}
