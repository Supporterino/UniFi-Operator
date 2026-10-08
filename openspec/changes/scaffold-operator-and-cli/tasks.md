## 1. Operator module scaffold

- [ ] 1.1 Run `kubebuilder init --domain supporterino.de --repo github.com/Supporterino/UniFi-Operator/operator` in `operator/` and verify `go build ./...` exits 0
- [ ] 1.2 Add a minimal `operator/.golangci.yml` and verify `golangci-lint run` exits 0
- [ ] 1.3 Verify `go vet ./...` exits 0 and the generated `Makefile` targets (`manifests`, `generate`, `test`, `build`) exist

## 2. UniFi client stub

- [ ] 2.1 Define a `Client` interface and plain result structs in `operator/internal/unifi/` (no Kubernetes types) and verify it compiles
- [ ] 2.2 Add an `httptest`-backed client with a fixture implementation and verify `go test ./internal/unifi/...` passes with no external network call
- [ ] 2.3 Document the stub's scope in a package comment and confirm `docs/unifi-api.md` remains the contract source (no code/doc drift)

## 3. Sample CRD and controller

- [ ] 3.1 Run `kubebuilder create api --group unifi --version v1alpha1 --kind UnifiNetwork --resource --controller` and verify the API type and reconciler are generated
- [ ] 3.2 Add `Spec`/`Status` fields with `+kubebuilder:subresource:status`, printer columns, and a fixture-backed client field; verify `make manifests generate` produces no diff
- [ ] 3.3 Implement idempotent reconcile with status conditions, `observedGeneration`, and upstream ID in `status` only; verify `go test ./...` passes
- [ ] 3.4 Add an envtest spec asserting the CRD applies, defaults/validates, and reports a `Ready` condition; verify `make test` passes
- [ ] 3.5 Add a reconciler unit test using the fake client asserting a second reconcile performs no writes; verify it passes

## 4. CLI module

- [ ] 4.1 Run `go mod init github.com/Supporterino/UniFi-Operator/cli` in `cli/` and verify `go build ./...` exits 0
- [ ] 4.2 Add a cobra root command and a `snapshot` subcommand stub that emits a `UnifiNetwork` CR; verify `go run . --help` and `go run . snapshot --help` exit 0
- [ ] 4.3 Add a unit test that runs `snapshot` against an `httptest` fixture and asserts the emitted CR `spec` contains no opaque upstream ID; verify `go test ./...` passes

## 5. Deployment manifests

- [ ] 5.1 Verify `kustomize build operator/config/default` exits 0 and renders the manager Deployment, RBAC, and the `UnifiNetwork` CRD
- [ ] 5.2 Author `charts/unifi-operator/` (Chart.yaml, values.yaml, templates for manager/RBAC/CRDs) and verify `helm lint charts/unifi-operator` and `helm template unifi-operator charts/unifi-operator -f charts/unifi-operator/values.yaml` exit 0
- [ ] 5.3 Diff the Helm-rendered core objects against the kustomize output and reconcile any drift

## 6. Cross-cutting verification

- [ ] 6.1 Run the operator gate (`go build ./... && go vet ./... && golangci-lint run && go test ./...` and `make manifests generate` no-diff) and confirm it passes
- [ ] 6.2 Run the CLI gate (`go build ./... && go vet ./... && go test ./...`) and confirm it passes
- [ ] 6.3 Run `zensical build --strict` and confirm the docs site still builds with no broken links
- [ ] 6.4 Run `openspec validate scaffold-operator-and-cli --strict` and confirm the change is valid
