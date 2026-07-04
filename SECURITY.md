# Security Policy

NorthScope is a read-only Kubernetes topology debugger. Security is especially important because the application runs inside a cluster and watches cluster resources.

## Supported Versions

NorthScope is currently pre-beta. Security fixes target the `main` branch and the latest published container image.

## Reporting a Vulnerability

Please do not open a public issue for security vulnerabilities.

Use GitHub private vulnerability reporting if it is enabled for this repository. If private reporting is not available, contact the maintainer listed in [MAINTAINERS.md](MAINTAINERS.md) before sharing public details.

Helpful report details:

- Affected version or commit
- Deployment method and Helm values, if relevant
- Impacted Kubernetes permissions or exposed data
- Reproduction steps
- Suggested mitigation, if known

## Security Model

NorthScope should remain read-only:

- Kubernetes RBAC should use only `get`, `list`, and `watch`.
- The application should not exec into Pods, proxy traffic, mutate resources, or create CRDs.
- The UI should not expose secrets or sensitive object data.
- The container should run as non-root with a minimal runtime image.

Changes that weaken this model require explicit maintainer review.
