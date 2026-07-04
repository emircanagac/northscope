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
- Simple Helm chart plus static manifests for installation
- EndpointSlice-aware backend checks, including selector-less Services

## Quick Start

Install NorthScope with Helm:

```bash
helm upgrade --install northscope ./charts/northscope \
  --namespace northscope \
  --create-namespace
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

Uninstall:

```bash
helm uninstall northscope -n northscope
```

Expose it through your ingress controller:

```bash
helm upgrade --install northscope ./charts/northscope \
  --namespace northscope \
  --create-namespace \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.hosts[0].host=northscope.example.com
```

Before exposing it publicly, set the ingress class, hostname, TLS, and any DNS/controller annotations your cluster requires.

You can also install the static manifests directly:

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

Or expose the manifest install through your ingress controller:

```bash
kubectl apply -f deploy/ingress.yaml
```

Before applying, edit `deploy/ingress.yaml` and set:

- `spec.ingressClassName` to your ingress controller class
- `spec.rules[0].host` to the hostname you want

If ExternalDNS is installed and your cluster requires explicit hostname annotations, add the annotation your DNS controller expects. Otherwise, create the DNS record manually and point it at your ingress controller or load balancer.

Uninstall the static manifests:

```bash
kubectl delete -f deploy/ingress.yaml --ignore-not-found
kubectl delete -f deploy/install.yaml
```

NorthScope only needs read-only Kubernetes permissions. The Helm chart and static manifest create a `ServiceAccount`, `ClusterRole`, `ClusterRoleBinding`, `Deployment`, and `Service`.

The default install uses this image:

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
