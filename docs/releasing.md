# Releasing

This is the canonical reference for how UniFi-Operator versions and publishes a release. The
operator, the CLI, and the Helm chart ship as **one bundle** under a single semantic version tag
`vX.Y.Z`; a release is defined by that tag plus the GitHub Release release-please creates for it.
Local build and packaging tasks live in [Building](building.md); the deployment paths that consume
the artifacts live in [Deployment](deployment.md).

## The tag flow

Releases are driven by [release-please](https://github.com/googleapis/release-please) from
conventional commits on `main`:

1. **Land releasable commits on `main`.** Each `feat:`/`fix:`/`perf:` commit (or a `!` breaking
   change) that reaches `main` is a candidate for the next version.
2. **release-please opens or updates a release PR.** `.github/workflows/release-please.yml` runs on
   every push to `main` and maintains a single release PR that proposes the next version.
   `bump-minor-pre-major: true` keeps the bundle in `0.x` until a 1.0 is chosen, and
   `include-component-in-tag: false` makes the tag `vX.Y.Z` with no component prefix.
3. **The release PR edits the versioned files.** Merging it updates `.release-please-manifest.json`,
   `CHANGELOG.md`, and `charts/unifi-operator/Chart.yaml` (`version` and `appVersion`, through the
   `extra-files` entries in `.release-please-config.json`) to the same `X.Y.Z`.
4. **Merge the release PR.** release-please tags the merge commit `vX.Y.Z` and creates the GitHub
   Release for it.
5. **The release workflow publishes the artifacts.** `.github/workflows/release.yml` fires on
   `release: published`, derives `X.Y.Z` from the tag, and runs the image, CLI, chart, and
   install-bundle jobs described below. It can also be re-run manually with
   `workflow_dispatch` and an explicit tag.

### Why `RELEASE_PLEASE_TOKEN` is required

GitHub suppresses workflow runs for events created with the default `secrets.GITHUB_TOKEN`: a
release published by an action using that token does **not** emit a `release: published` event to
other workflows (this prevents recursive runs). release-please would therefore create the tag and
Release, and the release workflow would never start.

`.github/workflows/release-please.yml` authenticates with `${{ secrets.RELEASE_PLEASE_TOKEN }}` — a
PAT or GitHub App token with `contents: write`, `issues: write`, and `pull-requests: write` —
instead of `GITHUB_TOKEN`, so the published Release propagates as an event and triggers the release
workflow. (`issues: write` is required because release-please applies its `autorelease: pending` /
`autorelease: tagged` labels through the Issues API.) A classic PAT with the `repo` scope also
covers all three. The secret must exist in the repository before the first release; without it
release-please cannot push the release branch or open the PR.

## What publishes where

All four artifacts are keyed off the same `X.Y.Z`:

| Artifact | Registry / location | Tag or name | Notes |
|----------|---------------------|-------------|-------|
| Container image | `ghcr.io/supporterino/unifi-operator` | `:X.Y.Z` and `:X.Y` | Multi-architecture (`linux/amd64`, `linux/arm64`); built with `VERSION=X.Y.Z`, so the manager reports the release version. |
| CLI binaries | the GitHub Release for `vX.Y.Z` | `unifi-operator-cli_X.Y.Z_<os>_<arch>.tar.gz` (`.zip` on Windows) plus `checksums.txt` | Cross-compiled by goreleaser from `cli/`, with `X.Y.Z` injected via `-ldflags`. |
| Helm chart | `oci://ghcr.io/supporterino/charts` | `unifi-operator:X.Y.Z` | Packaged from `charts/unifi-operator` as release-please left it; `appVersion` is `X.Y.Z`. |
| Install bundle | the GitHub Release for `vX.Y.Z` | `install.yaml` | Consolidated kustomize manifest from `operator/config/default` with the image pinned to `:X.Y.Z`. |

The release workflow is split into one job per artifact so each carries only the permission it
needs:

| Job | Artifact | Permissions |
|-----|----------|-------------|
| `version` | derives `X.Y.Z`, `X.Y`, and the tag for the other jobs | `contents: read` |
| `image` | multi-arch image to ghcr.io | `contents: read`, `packages: write` |
| `cli` | CLI archives and checksums, attached to the existing Release | `contents: write` |
| `chart` | chart to the OCI registry | `contents: read`, `packages: write` |
| `install-bundle` | `install.yaml`, attached to the existing Release | `contents: write` |

The CLI and install-bundle jobs attach to the Release release-please already created; `.goreleaser.yaml`
sets `release.mode: append` precisely so goreleaser does not open a second Release. `gh release upload`
follows the same rule.

## Annotated chart version vs. injected image tag

Two different mechanisms put a version on the artifacts, and they are deliberately not the same:

- **The chart version is annotated.** release-please rewrites `charts/unifi-operator/Chart.yaml`
  `version` and `appVersion` in the repository, so any checked-out tree states its version truthfully
  and the release workflow only has to `helm package` and `helm push` it. The packaged chart reports
  the tag's version.
- **The image tag is injected.** The image does not exist until the release workflow builds it, so a
  committed image tag in `operator/config/manager/manager.yaml` or `charts/unifi-operator/values.yaml`
  cannot be authoritative — that would be circular. Those files therefore carry the real image
  *repository* with an annotated placeholder/default tag, and the release workflow supplies the real
  tag:
  - the kustomize path pins it when building `install.yaml` (`VERSION=X.Y.Z task build-installer`);
  - the image job tags the pushed image itself;
  - the chart leaves `image.tag` empty so a released chart's Deployment falls back to its own
    `appVersion`, which release-please has set to `X.Y.Z`.

In short: the chart is *annotated* with `X.Y.Z` in git; the image tag is *injected* at build and
deploy time from the same tag. See [Deployment](deployment.md) for how the tag is overridden at
install time.

## Rollback

Version facts and published artifacts are additive, so a release can be undone without rewriting
history:

1. **Revert the change.** Revert the merge commit whose commit (and content) should not have been
   released, restore `.release-please-manifest.json` and `charts/unifi-operator/Chart.yaml` to the
   previous version, and merge that revert. release-please then continues from the restored version.
2. **Delete the tag and Release if needed.** The `vX.Y.Z` tag and its GitHub Release can be deleted
   independently; nothing else references the tag once the artifacts below are handled.
3. **Delete the artifacts independently.** Image tags and the chart version can be removed from
   ghcr.io on their own; the Release assets disappear with their Release. No artifact is
   single-writer, so any one can be deleted without touching the others.
4. **Cut a new tag.** Prefer a fresh patch release over reusing a deleted `vX.Y.Z`: consumers that
   already pulled an artifact may cache it by tag.

`operator/config/` and the old manifests restore from git history, so reverting the manifest
defaults is just another revert of the same change.

## Related

- [Building](building.md) — the local tasks that produce the same artifacts (`build-installer`,
  `image`, `docker-buildx`).
- [Deployment](deployment.md) — consuming the published image and chart.
- [Documentation](documentation.md) — how this page is built and published.
