# Security Bar

This is the canonical security bar for the UniFi-Operator workspace. It is cited by the
`go-developer` and `go-reviewer` agents and referenced by [Go bar](go.md). It states the
invariants every change must preserve. Kubernetes-specific mechanics (RBAC markers, manager
ServiceAccount) live in `operator/AGENTS.md`.

## The bar

### UniFi controller credentials

- **Never commit credentials.** Unifi API keys, controller usernames/passwords, and kubeconfigs
  stay out of the tree — including sample CRs and docs.
- **Read from a `Secret`.** Credentials are read from a Kubernetes `Secret` referenced by the
  operator configuration or referenced from a CR. Never a checked-in fallback or default
  password.
- **Never log secrets.** The UniFi client and reconcilers must not log API keys, passwords,
  session cookies, or tokens. Redact before logging; do not `klog`/`logr` a whole request or
  config struct.
- **Keep credentials out of `status`.** A CR's `status` may hold a controller object ID for
  correlation, but never a credential or session token.

### Kubernetes RBAC

- **Least privilege.** Grant only the verbs and resources the operator uses. Add
  `+kubebuilder:rbac` markers next to the calls that need them and re-run `make manifests`;
  do not hand-edit `config/rbac/`.
- **Scope cluster-wide verbs deliberately.** `ClusterRole`/`ClusterRoleBinding` is a red flag;
  prefer namespaced `Role` where the resource is namespaced. Justify any cluster-scoped access
  in the change.
- **Status is a subresource.** The operator updates status through the status subresource with
  the `status` verb; it does not need blanket `update` on the object for status-only changes.

### Secret hygiene

- **Do not copy Secrets into CRs or ConfigMaps.** Reference a `Secret` by name; resolve at
  reconcile time.
- **Do not put Secret data in events or logs.** Error messages that reach events must not echo
  credential material.
- **Mark credential fields `+kubebuilder:validation:Optional` and `// +sensitive` intent** where
  a CR carries a value the controller should treat as sensitive; prefer a `Secret` reference
  over an inline value.

### Input validation

- **Validate at the API boundary.** CR validation is expressed with `+kubebuilder:validation:*`
  markers so the API server rejects bad `spec`s before the controller sees them. Only use a
  validating webhook for genuinely cross-field rules.
- **Never trust controller responses.** Parse UniFi API responses into typed structs and handle
  missing/extra fields defensively; do not assume the upstream shape is valid.
- **Fail closed.** On an auth or validation error, set a failing condition and do not proceed to
  mutate the controller. Do not fall through to a permissive default.

### Injection and data safety

- **No shell injection.** If the operator or CLI ever spawns a process (for example the CLI
  shelling out), pass arguments as arrays, never a shell string built from input.
- **Constrain file access.** CLI paths from users are resolved against a known base and checked;
  do not read or write arbitrary paths on request.
- **No unsafe URL construction.** Build controller URLs from a validated base URL and path
  segments, not string concatenation of user input, to avoid SSRF/redirection.
- **TLS.** Verify controller TLS by default. A skip-verify option must be explicit and
  documented as insecure.

## Verification

A change touching auth, RBAC, request parsing, credential handling, or logging is not done
until these scenarios are checked and the module gate (see [Go bar](go.md) and the
`go-developer` agent) passes.

## Related

- [Go bar](go.md)
- [UniFi API contract](unifi-api.md)
- [Architecture](architecture.md)
