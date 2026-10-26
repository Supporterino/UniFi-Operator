# Spec Delta

## Purpose

Define what the UniFi operator must export about its own health, how both deployment paths make
that signal scrapeable, and the cardinality, credential, and TLS constraints the exported signal
must respect. This capability describes operator observability, not the state of the UniFi console.

## ADDED Requirements

### Requirement: Reconcile outcomes are exported with their reason

The operator SHALL count every terminal reconcile outcome in a monotonic counter named
`unifi_operator_reconcile_total`, labelled with the Custom Resource kind, the outcome
(`ready` or `not_ready`), and the reason recorded on the object's `Ready` condition. The exported
reason MUST equal the reason written to status for the same reconcile, so a counter value can be
traced back to a condition without reading the object.

#### Scenario: A failing reconcile is attributable to its reason

- **WHEN** a reconcile finishes by writing `Ready=False` with reason `AuthenticationFailed`
- **THEN** `unifi_operator_reconcile_total` increases by one for that kind with
  `result="not_ready"` and `reason="AuthenticationFailed"`

#### Scenario: A successful reconcile is counted independently

- **WHEN** a reconcile finishes by writing `Ready=True`
- **THEN** the counter increases with `result="ready"` and the reason reported in status, and no
  `not_ready` series is incremented

#### Scenario: A retried transient error is not reported as a terminal failure

- **WHEN** a reconcile returns a transient upstream error without writing status
- **THEN** no `unifi_operator_reconcile_total` series changes

### Requirement: Upstream traffic is exported by bounded operation
The operator SHALL count requests to the UniFi controller in `unifi_operator_upstream_requests_total`,
labelled with a bounded logical operation and an outcome (`success` or `error`), and SHALL record
their latency in the histogram `unifi_operator_upstream_request_duration_seconds` labelled with the
same operation. The operation label MUST identify the API surface, never a concrete URL.

#### Scenario: A failed controller request increments the error outcome

- **WHEN** a client call to a firewall policy endpoint fails with an API error
- **THEN** the request counter increases with the firewall-policy operation and `outcome="error"`,
  and the latency histogram observes the failed call

#### Scenario: Requests to different sites share one operation label

- **WHEN** the same endpoint is called for two different sites
- **THEN** both requests increment the same `operation` series, so no site identifier appears as a
  label value

### Requirement: Build identity is exported

The operator SHALL export `unifi_operator_build_info` as a gauge fixed at `1` with a single
`version` label carrying the build version stamped at link time, so a scraped series can be
attributed to a release. The version MUST NOT be added as a label to any other exported metric.

#### Scenario: A scrape identifies the running build

- **WHEN** the operator built from version `v1.2.3` serves metrics
- **THEN** `unifi_operator_build_info{version="v1.2.3"} 1` is exposed

#### Scenario: Upgrading does not churn the other series

- **WHEN** the operator is upgraded from one version to another
- **THEN** `unifi_operator_reconcile_total` and the upstream metrics keep their existing label sets,
  and only `unifi_operator_build_info` changes

### Requirement: Condition transitions emit Kubernetes Events

The operator SHALL record a Kubernetes Event against the reconciled object when its `Ready`
condition changes: a `Warning` Event when it becomes `False`, with the condition reason as the Event
reason, and a `Normal` Event when it returns to `True`. It MUST NOT emit an Event when the condition
is unchanged, MUST NOT emit one per retry or per resync, and the Event message MUST NOT contain
credential material.

#### Scenario: A first failure is recorded once

- **WHEN** a reconcile writes `Ready=False` with reason `SecretNotFound` where the condition was
  previously `True`
- **THEN** one `Warning` Event with reason `SecretNotFound` is recorded against the object

#### Scenario: A repeated failure is not re-recorded

- **WHEN** a subsequent reconcile writes the same `Ready=False` and the same reason
- **THEN** no new Event is recorded for that transition

#### Scenario: Recovery is recorded

- **WHEN** a failing object reconciles successfully and its condition returns to `True`
- **THEN** one `Normal` Event is recorded against the object

#### Scenario: Event emission does not leak credentials

- **WHEN** a reconcile fails while resolving a credential Secret
- **THEN** the Event message names the Secret and key at most, and contains no credential value



Every metric the operator exports SHALL use a closed, enumerable label set. No metric SHALL use a
label whose value includes a Custom Resource name, a namespace, an upstream UUID or `_id`, a URL or
path segment, a condition message, or any credential material.

#### Scenario: No upstream identifier is exported

- **WHEN** any metric is scraped while objects for multiple sites and controllers are reconciled
- **THEN** no label value in the exposure is a UUID, a site identifier, a resource name, or a
  namespace

#### Scenario: A long or hostile message does not reach a label

- **WHEN** a reconcile fails with an error text containing upstream response detail
- **THEN** only the fixed reason code appears as a label value, and the message stays in status and
  logs

### Requirement: Both deployment paths expose an opt-in scrape configuration

The operator SHALL be scrapeable through a Prometheus Operator `ServiceMonitor` on both the
kustomize and the Helm deployment path, and the two paths SHALL expose equivalent scrape settings
(endpoint, port, TLS, interval). The scrape object MUST be disabled by default, and installing the
operator through either path MUST NOT require the Prometheus Operator's custom resource
definitions to be installed.

#### Scenario: Kustomize install without the monitoring CRDs succeeds

- **WHEN** the default kustomize overlay is applied to a cluster that has no
  `monitoring.coreos.com` custom resource definitions
- **THEN** the apply succeeds and no ServiceMonitor is created

#### Scenario: Helm install renders no ServiceMonitor by default

- **WHEN** the chart is rendered with its default values
- **THEN** the output contains the metrics Service but no ServiceMonitor

#### Scenario: Enabling the scrape object in either path yields an equivalent target

- **WHEN** the kustomize scrape overlay is applied, or the chart is installed with the ServiceMonitor
  enabled
- **THEN** the resulting ServiceMonitor selects the operator's metrics Service on the HTTPS metrics
  port with the same path, scheme, and authentication mechanism as the other path

### Requirement: Scraping verifies TLS by default

The shipped scrape configuration SHALL verify the metrics endpoint's certificate. A configuration
that skips verification MUST be a separately named, explicitly opt-in option, and its documentation
MUST state that it is insecure. No default manifest or default chart value SHALL disable certificate
verification.

#### Scenario: Default scrape configuration verifies the certificate

- **WHEN** the default kustomize scrape overlay or the default chart values are rendered
- **THEN** the ServiceMonitor does not set `insecureSkipVerify: true`

#### Scenario: The bypass is opt-in and labelled insecure

- **WHEN** a user applies the self-signed overlay, or sets the chart's skip-verification value
- **THEN** the rendered ServiceMonitor skips verification, and the documentation names that choice
  as insecure

### Requirement: The observability contract is documented

The documentation SHALL list the exported metric names and their labels, describe which transitions
produce Kubernetes Events, state that the scraping service account must be bound to the existing
`metrics-reader` ClusterRole, and give at least one example alerting expression keyed on a condition
reason. The documentation MUST NOT claim that metrics scraping or Kubernetes Events are enabled by
default when they are not.

#### Scenario: A user can wire an alert from the documentation alone

- **WHEN** a user follows the deployment documentation's observability section
- **THEN** they find the metric names, the required role binding, and a working alert expression
  built from the reason label

#### Scenario: A user can find a failure with kubectl

- **WHEN** a user follows the debugging documentation after a failing condition
- **THEN** they are told which Events to expect and which metric query surfaces the same failure

#### Scenario: Documentation matches the shipped defaults

- **WHEN** the documented defaults are compared with the rendered kustomize and Helm output
- **THEN** every observability surface described as enabled is enabled, every opt-in surface is
  described as opt-in, and the required `events` RBAC verb is documented
