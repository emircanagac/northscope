# NorthScope

![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)
![Go](https://img.shields.io/badge/go-1.22+-00ADD8.svg)
![Build Status](https://github.com/emircanagac/northscope/actions/workflows/ci.yml/badge.svg)

**A lightweight, read-only Kubernetes Ingress traffic path debugger.**

NorthScope visualizes configured north-south Kubernetes traffic paths without changing your cluster. It watches standard Kubernetes API resources, groups traffic by Ingress and host, and shows the path from the assumed external load balancer entry to the controller, Ingress, Service, and backend Pods. Optional Gateway API and F5 CIS resources are discovered as supporting context when their CRDs are installed.

## Why NorthScope?

Modern Kubernetes ingress debugging can become slow quickly: you need to connect controller, host, path, backend Service, Service port, EndpointSlice, Pod readiness, and Node placement before you know where to look. NorthScope keeps the default view intentionally simple, so the first screen answers: "for this host, where is traffic supposed to go?"

NorthScope does **not** use:

- eBPF
- DaemonSet agents
- sidecars
- service mesh dependencies
- custom CRDs
- write permissions

Instead, it uses the boring, reliable path: Kubernetes API `get`, `list`, and `watch` access through `client-go` informers plus read-only dynamic discovery for optional CRDs.

## Features

- Namespace-scoped Ingress traffic visualization grouped as `Ingress + host -> paths`
- Simple mode: `F5 / LB -> NodePort if present -> Controller -> Ingress -> Service -> Pod summary`
- Expanded mode for deeper debugging with host/route, individual Pod, Node, and EndpointSlice context
- Route-level diagnosis for missing Services, missing Service ports, selector mismatches, no Ready Pods, missing EndpointSlices, and unusable endpoints
- Gateway API discovery for GatewayClass, Gateway, HTTPRoute, GRPCRoute, TLSRoute, TCPRoute, and UDPRoute as optional context
- F5 CIS discovery for IngressLink, VirtualServer, and TransportServer as optional context
- Read-only by design: no mutating Kubernetes API calls
- Real-time updates over WebSocket, with periodic refresh for optional dynamic CRDs
- Interactive React Flow UI with namespace selection, host/path groups, Simple mode, and Expanded mode
- Single Go binary with embedded Vite/React frontend
- Single container image, no extra in-cluster agents
- Helm chart for installation
- EndpointSlice-aware backend checks, including selector-less Services

## Quick Start

Fastest way to try NorthScope:

```bash
helm upgrade --install northscope ./charts/northscope \
  --namespace northscope \
  --create-namespace
kubectl -n northscope rollout status deploy/northscope
kubectl -n northscope port-forward svc/northscope 8080:80
```

Open:

```text
http://localhost:8080
```

Remove it:

```bash
helm uninstall northscope -n northscope
```

## Install Options

The Helm chart is the source of truth for installing NorthScope while the project is in pre-beta validation.

Expose NorthScope through your ingress controller:

```bash
helm upgrade --install northscope ./charts/northscope \
  --namespace northscope \
  --create-namespace \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.hosts[0].host=northscope.example.com
```

NorthScope only needs read-only Kubernetes permissions. The Helm chart creates a `ServiceAccount`, `ClusterRole`, `ClusterRoleBinding`, `Deployment`, and `Service`. The default image is:

```text
ghcr.io/emircanagac/northscope:latest
```

## Naming

The project is standardized on **NorthScope** as the display name and `northscope` for repository, module, binary, Kubernetes resources, and container image paths.

The image is published automatically by GitHub Actions on every push to `main`. If the repository or GHCR package is private, either make the package public or configure an image pull secret in your cluster before applying the manifest.

## Repository Layout

```text
.github/workflows/   GitHub Actions CI and image publishing
charts/northscope/   Helm chart, the source of truth for installation
cmd/northscope/      Go binary entrypoint
internal/k8s/        Kubernetes watchers, discovery, and topology building
internal/models/     Shared API and topology models
internal/server/     HTTP server, health checks, and WebSocket stream
ui/                  React UI embedded into the Go binary
```

NorthScope follows the usual Go application layout: `cmd/northscope` contains the executable entrypoint, while `internal` contains private backend packages. The frontend stays under `ui` because it is shipped as an embedded UI inside the same binary rather than as a separate service.

## Local Development

NorthScope has a Go backend and a Vite React frontend under `ui/`.

Useful commands:

```bash
make ui-build
```

Builds only the frontend with `npm run build`.

```bash
make build
```

Builds the frontend, embeds `ui/dist`, and compiles the Go binary into `bin/northscope`.

```bash
make run
```

Runs NorthScope locally using your developer kubeconfig at `~/.kube/config`.

```bash
make docker
```

Builds a local Docker image tagged as `ghcr.io/emircanagac/northscope:dev`.

You can override defaults:

```bash
IMAGE=ghcr.io/emircanagac/northscope:dev make docker
KUBECONFIG=/path/to/kubeconfig make run
```

## Architecture

```text
Kubernetes API
  |-- client-go SharedInformers
  |   |-- Ingress / IngressClass
  |   |-- Service
  |   |-- EndpointSlice
  |   |-- Pod
  |   `-- Node
  `-- dynamic read-only discovery
      |-- Gateway API resources
      `-- F5 CIS resources
          |
Topology Builder
          |
Go HTTP + WebSocket Server
          |
Embedded React Flow UI
```

The frontend is compiled with Vite and embedded into the Go backend using `//go:embed`, so production deployment ships as one binary inside one container.

## Security Model

NorthScope is intentionally read-only. The default ClusterRole grants only:

```text
get, list, watch
```

It does not create, patch, update, delete, exec into, or proxy through workloads.

## Community

- [Contributing](CONTRIBUTING.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Governance](GOVERNANCE.md)
- [Maintainers](MAINTAINERS.md)
- [Security Policy](SECURITY.md)
- [Support](SUPPORT.md)
- [Changelog](CHANGELOG.md)
- [Roadmap](ROADMAP.md)

## Project Status

NorthScope is in pre-beta validation. The core Ingress topology workflow is usable, but the project still needs more real-cluster screenshots, installation feedback, and scenario testing before a v0.1.0 beta release. Its primary workflow is Kubernetes Ingress debugging: pick a namespace, select an Ingress+host group, and inspect the configured traffic path. Multiple paths under the same host are drawn together because they represent one host entry point.

It is intentionally observational: NorthScope shows what Kubernetes configuration says should happen. It does not replace packet tracing, cloud load balancer inventory, live flow telemetry, or controller-specific diagnostics. The `F5 / LB` entry is an assumed external edge unless Kubernetes resources expose richer context. Gateway API and F5 CIS objects are currently used as discovered context, not as a replacement for provider/controller-specific tooling.

Recommended validation scenarios:

- one Ingress, one host, multiple paths
- two different hosts in the same namespace
- the same host used by different Ingress objects
- NodePort and LoadBalancer ingress controller Services
- missing backend Service, missing Service port, and zero Ready Pods

## License

Apache License 2.0. See [LICENSE](LICENSE).
