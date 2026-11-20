# Contributing to NorthScope

Thanks for considering a contribution. NorthScope is pre-beta, so the most valuable help right now is real-cluster validation, topology edge cases, documentation fixes, and focused bug reports.

## Ways to Contribute

- Report a topology bug with a minimal Kubernetes example.
- Share screenshots or GIFs from real clusters.
- Add tests for Ingress, Gateway API, F5 CIS, Service, endpoint, and controller edge cases.
- Improve Helm chart defaults and installation docs.
- Improve the UI when a topology is large, missing data, or unhealthy.

## Development Setup

Requirements:

- Go 1.27.4+
- Node.js 26+
- npm
- Docker, optional
- A Kubernetes cluster or kubeconfig, optional for local UI/backend testing

Build and test:

```bash
make ui-build
npm --prefix ui run lint
npm --prefix ui run test:ui-smoke
npm --prefix ui run test:e2e
go test ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...
make build
```

CI runs the same checks. Go code is linted with [golangci-lint](https://golangci-lint.run/) (`.golangci.yml`) and the UI with [oxlint](https://oxc.rs/docs/guide/usage/linter.html) (`ui/.oxlintrc.json`), including the React hooks rules.

Install the Playwright Chromium runtime once before running browser tests locally:

```bash
npm --prefix ui exec playwright install chromium
```

Run locally with your kubeconfig:

```bash
make run
```

Run against a generated in-memory cluster, without Kubernetes. A few routes are broken on purpose, and a Pod changes readiness every few seconds so live updates show. Run `make ui-build` first to serve the built UI on port 8080, or run `npm --prefix ui run dev` alongside it for a live-reloading UI on port 5173:

```bash
go run ./hack/demo
go run ./hack/demo -namespaces 200 -services-per-namespace 10 -pods-per-service 10 -nodes 200
```

Measure snapshot builds, change detection, deltas, and cached object sizes at 100, 2,000, and 20,000 Pods:

```bash
go test ./internal/k8s -run '^$' -bench . -benchmem
```

Build the container image:

```bash
make docker
```

## Pull Request Guidelines

- Keep PRs focused. One behavior change per PR is easier to review.
- Add or update tests when changing topology building, watcher behavior, or API models.
- Update README or Helm values when changing install behavior.
- Prefer existing package boundaries: `internal/k8s`, `internal/server`, `internal/models`, and `ui`.
- Do not add write permissions to Kubernetes RBAC unless the project direction explicitly changes.

## Commit and License

By contributing, you agree that your contribution is provided under the Apache License 2.0.

NorthScope does not currently require DCO sign-off or a CLA. If the project grows toward a foundation-style governance model, this may be revisited.
