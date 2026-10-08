# Documentation

This is the canonical reference for how the UniFi-Operator documentation site is built and
published. The docs are first-class: they are versioned with the code and deployed automatically.

## Stack

- **Markdown** under `docs/`.
- **[Zensical](https://zensical.org/)** as the static site generator, configured by
  `zensical.toml` at the repository root.
- **GitHub Pages** as the host, deployed by `.github/workflows/docs.yml`.

The built site outputs to `site/`, which is git-ignored.

## Local development

```bash
# Preview with live reload
zensical serve

# Strict build — the same gate CI runs
zensical build --strict
```

`-strict` fails the build on broken internal links and missing nav entries, so it is the gate:
a docs change is not done until `zensical build --strict` is green.

## Navigation

The `nav` list in `zensical.toml` is the site's table of contents. **Every page under `docs/`
must be reachable from `nav`** — Zensical's strict mode treats an orphaned page as an error in
CI. When you add a page, add it to `nav` in the same change.

## Writing conventions

- File names are `kebab-case.md`.
- Each page opens with a single `#` title, then a short orientation paragraph.
- Use relative `.md` links between pages so strict mode validates them.
- Prefer tables for rules and command references.
- Code blocks are fenced with a language tag.

## Single source of truth

Do not restate the same fact in two pages. The canonical homes are:

| Fact | Canonical document |
|------|--------------------|
| Architecture and boundaries | `architecture.md` |
| CRD design rules | `crd-conventions.md` |
| UniFi controller contract | `unifi-api.md` |
| Go language bar | `go.md` |
| Security bar | `security.md` |
| Testing workflow | `testing.md` |
| Debugging workflow | `debugging.md` |
| Deployment (kustomize + Helm) | `deployment.md` |
| Docs pipeline (this page) | `documentation.md` |

Cross-references link to the canonical page rather than copying its content.

## Publishing

`.github/workflows/docs.yml` builds the site with `zensical build` on pushes to `main` and
deploys it to GitHub Pages via `actions/deploy-pages`. The GitHub repository setting
**Settings → Pages → Build and deployment → Source** must be set to **GitHub Actions**.

If the site is served from a project path (`https://<owner>.github.io/UniFi-Operator/`), set
`site_url` in `zensical.toml` accordingly so canonical links resolve.

## Related

- Root `README.md` — human overview.
- Root `AGENTS.md` — agent-facing index of the reference library.
