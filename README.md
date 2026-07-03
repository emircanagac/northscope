# NorthScope

![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)
![Go](https://img.shields.io/badge/go-1.22+-00ADD8.svg)
![Build Status](https://github.com/emircanagac/northscope/actions/workflows/ci.yml/badge.svg)

**A lightweight, read-only Kubernetes north-south topology observer.**

NorthScope visualizes north-south Kubernetes traffic paths without changing your cluster. It watches standard Kubernetes API resources, optionally reads Gateway API and F5 CIS resources when their CRDs are installed, builds a live topology graph, and serves a React Flow UI from a single Go binary.

## Why NorthScope?

Modern Kubernetes networking can become hard to reason about quickly: DNS, external load balancers, Gateway API, Ingress, controllers, NodePorts, Services, EndpointSlices, Nodes, and Pods all sit in the request path. NorthScope makes that path visible while keeping the cluster untouched.

NorthScope does **not** use:

- eBPF
- DaemonSet agents
- sidecars
- service mesh dependencies
- custom CRDs
- write permissions

Instead, it uses the boring, reliable path: Kubernetes API `get`, `list`, and `watch` access through `client-go` informers plus read-only dynamic discovery for optional CRDs.

## Features

- Live DNS, external load balancer, Gateway, Ingress, Service, Node, and Pod topology visualization
- Gateway API discovery for GatewayClass, Gateway, HTTPRoute, GRPCRoute, TLSRoute, TCPRoute, and UDPRoute
- F5 CIS discovery for IngressLink, VirtualServer, and TransportServer
- Read-only by design: no mutating Kubernetes API calls
- Real-time updates over WebSocket, with periodic refresh for optional dynamic CRDs
- Interactive React Flow graph with full-cluster view, search, namespace filtering, and optional Ingress focus
- Single Go binary with embedded Vite/React frontend
- Single container image, no extra in-cluster agents
- EndpointSlice fallback for selector-less Services

## Quick Start

Install NorthScope into your cluster:

```bash
kubectl apply -f deploy/install.yaml
```

Wait for the deployment:

```bash
kubectl -n northscope rollout status deploy/northscope
```

Open a local tunnel:

```bash
kubectl -n northscope port-forward svc/northscope 8080:80
```

Then open:

```text
http://localhost:8080
```

NorthScope only needs read-only Kubernetes permissions. The included manifest creates a `ServiceAccount`, `ClusterRole`, `ClusterRoleBinding`, `Deployment`, and `Service`.

The default manifest uses this image:

```text
ghcr.io/emircanagac/northscope:latest
```

## Naming

The project is standardized on **NorthScope** as the display name and `northscope` for repository, module, binary, Kubernetes resources, and container image paths.

The image is published automatically by GitHub Actions on every push to `main`. If the repository or GHCR package is private, either make the package public or configure an image pull secret in your cluster before applying the manifest.

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

## Project Status

NorthScope is ready for early open-source testing as a read-only observer. It covers the common north-south path across DNS, external load balancers, Gateway API, Ingress, Services, EndpointSlices, Nodes, and Pods, plus F5 CIS CRDs when present.

It is still intentionally observational: NorthScope does not replace packet tracing, cloud load balancer inventory, or controller-specific diagnostics. Provider metadata is inferred from Kubernetes status and optional CRD fields, so exact cloud/F5 configuration should still be verified in the owning platform.

## License

Apache License 2.0. See [LICENSE](LICENSE).
