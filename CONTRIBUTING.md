# Contributing to NorthScope

Thanks for considering a contribution. NorthScope is pre-beta, so the most valuable help right now is real-cluster validation, topology edge cases, documentation fixes, and focused bug reports.

## Ways to Contribute

- Report a topology bug with a minimal Kubernetes example.
- Share screenshots or GIFs from real clusters.
- Add tests for Ingress, Service, EndpointSlice, and controller edge cases.
- Improve Helm chart defaults and installation docs.
- Improve the UI when a topology is large, missing data, or unhealthy.

## Development Setup

Requirements:

- Go 1.22+
- Node.js 22+
- npm
- Docker, optional
- A Kubernetes cluster or kubeconfig, optional for local UI/backend testing

Build and test:

```bash
make ui-build
go test ./...
make build
```

Run locally with your kubeconfig:

```bash
make run
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
