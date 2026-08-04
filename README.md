# NorthScope

![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)
![Go](https://img.shields.io/badge/go-1.26.5+-00ADD8.svg)
![Build Status](https://github.com/emircanagac/northscope/actions/workflows/ci.yml/badge.svg)

**NorthScope is a lightweight, read-only Kubernetes north-south traffic topology debugger.**

NorthScope helps platform, SRE, DevOps, and application teams understand configured traffic paths without changing the cluster. It watches Kubernetes API resources with read-only access and visualizes Ingress, Gateway API, and F5 CIS roots through Services, endpoints, Pods, and Nodes.

![NorthScope simple topology view](docs/assets/northscope-simple-topology.gif)

## Overview

North-south traffic debugging often means stitching together several commands:

- which controller or Gateway owns this route?
- which host and path matched?
- which Service and port does it route to?
- are there Ready Pods or usable endpoints?
- which Node is running the backend?

NorthScope turns that configured traffic model into a UI. The default view stays intentionally simple:

```text
F5 / LB -> NodePort if present -> Controller / Gateway -> Route -> Service -> Pod summary
```

Expanded mode adds route, DNS/host, individual Pod, Node, EndpointSlice, and legacy Endpoint context for deeper debugging.

## Features

- Read-only Kubernetes topology discovery
- Namespace-aware traffic route browser
- Ingress object -> host -> path grouping
- First-class Gateway API and F5 CIS route roots when their CRDs are installed
- Simple and Expanded topology modes
- Route diagnostics for missing Services, missing ports, selector mismatches, no Ready Pods, missing EndpointSlice/Endpoints data, and unusable endpoints
- EndpointSlice-aware backend checks, including selector-less Services and legacy Endpoints fallback
- ReferenceGrant-aware cross-namespace Gateway API backends
- Coalesced Kubernetes event processing to avoid rebuild storms
- Traffic-root-scoped WebSocket snapshots with complete inventory counts for the configured watch scope
- Real-time updates over WebSocket, with unchanged snapshots suppressed
- Prometheus-compatible `/metrics` endpoint for watcher health and snapshot build status
- Single Go binary with embedded React UI
- Single container image and Helm chart

NorthScope does not require eBPF, DaemonSets, sidecars, service mesh dependencies, custom CRDs, or write permissions.

## Installation

Prerequisites:

- Kubernetes 1.30 or newer
- Helm 3

Add the Helm repository:

```bash
helm repo add northscope https://emircanagac.github.io/northscope
helm repo update
```

Install NorthScope:

```bash
helm upgrade --install northscope northscope/northscope \
  --namespace northscope \
  --create-namespace
```

For reproducible installs, pin a chart version with `--version`.

Check rollout:

```bash
kubectl -n northscope rollout status deploy/northscope
```

The chart prints the access command after installation. By default, NorthScope is installed as a ClusterIP Service and can be opened with port-forwarding. To expose it through DNS, configure the chart's `ingress` values for your ingress controller and protect access with your platform's TLS and authentication controls. See [Production Access](docs/production-access.md) for concise TLS, authentication, and NetworkPolicy examples.

Production hardening options such as `networkPolicy`, `podDisruptionBudget`, resources, tolerations, and affinity are available as chart values. See `charts/northscope/values.yaml` for the full list.

For troubleshooting and operational metrics, see [Troubleshooting](docs/troubleshooting.md).

Uninstall:

```bash
helm uninstall northscope -n northscope
```

## Demo Topology

For screenshots or local UI validation, apply the optional demo topology after installing NorthScope:

```bash
kubectl apply -f https://raw.githubusercontent.com/emircanagac/northscope/main/examples/demo-topology.yaml
```

Then select the `northscope` namespace in the UI. The demo creates multiple Ingress objects, hosts, paths, Services, Pods, selector-less external endpoints, unhealthy backend examples, and placeholder ingress-controller NodePort Services.

Remove only the demo resources:

```bash
kubectl delete -f https://raw.githubusercontent.com/emircanagac/northscope/main/examples/demo-topology.yaml
```

## Architecture

```text
Kubernetes API
  |-- client-go SharedInformers
  |   |-- Ingress / IngressClass
  |   |-- Service
  |   |-- EndpointSlice / Endpoints
  |   |-- Pod
  |   `-- Node
  `-- dynamic read-only discovery
      |-- Gateway API resources
      `-- F5 CIS resources
          |
Debounced Topology Builder
          |
Traffic-root-scoped Snapshot + Watch-scope Inventory
          |
Go HTTP + WebSocket Server
          |
Embedded React Flow UI
```

The frontend is compiled with Vite and embedded into the Go backend using `//go:embed`, so production deployment ships as one binary inside one container.

## Support And Status Semantics

| Source | Supported API surface |
| --- | --- |
| Kubernetes Ingress | `networking.k8s.io/v1` Ingress and IngressClass |
| Gateway API | Served `v1`, `v1beta1`, and supported `v1alpha2` Gateway, route, and ReferenceGrant resources |
| F5 CIS | `cis.f5.com/v1` VirtualServer, TransportServer, and IngressLink |
| Service backends | ClusterIP, NodePort, LoadBalancer, ExternalName, EndpointSlice, and legacy Endpoints |

NorthScope reports the state represented by Kubernetes API objects. Labels such as `Configured`, `Backends ready`, and `Error` are configuration-derived diagnostics, not active network probes. NorthScope does not send requests through application routes, inspect controller logs, or claim that an external F5/LB is reachable.

Gateway API and F5 discovery are enabled by default but only start when their API resources are served. They can be disabled with `discovery.gatewayAPI.enabled=false` or `discovery.f5.enabled=false`. Set `watchNamespace` to one namespace when cluster-wide route discovery is not required; namespaced RBAC and inventory are then limited to that namespace while Node and class metadata remain cluster-scoped.

## Security

NorthScope is intentionally observational. The default ClusterRole grants only:

```text
get, list, watch
```

It does not read Secrets, ConfigMaps, Pod logs, or Events. It does not create, patch, update, delete, exec into, or proxy through workloads. Because topology data can reveal internal hostnames, Services, Pods, Nodes, and IPs, run NorthScope behind trusted internal access controls. See [SECURITY.md](SECURITY.md) for the exact RBAC surface and [Production Access](docs/production-access.md) for TLS/auth guidance.

## Project Status

NorthScope is in pre-beta validation. Ingress, Gateway API, and F5 CIS topology workflows are usable, but the project still needs broader real-cluster compatibility, scale, and installation feedback before a beta release.

Recommended validation scenarios:

- one Ingress, one host, multiple paths
- one Ingress object with multiple hosts
- the same host used by different Ingress objects
- NodePort and LoadBalancer ingress controller Services
- selector-less Services with manually managed EndpointSlices or legacy Endpoints
- missing backend Service, missing Service port, and zero Ready Pods
- Gateway API cross-namespace backends with and without a ReferenceGrant
- F5 CIS VirtualServer, TransportServer, and IngressLink resources

## Development

Repository layout:

```text
.github/workflows/   GitHub Actions CI, image publishing, and chart publishing
charts/northscope/   Helm chart
cmd/northscope/      Go binary entrypoint
internal/k8s/        Kubernetes watchers, discovery, and topology building
internal/models/     Shared API and topology models
internal/server/     HTTP server, health checks, and WebSocket stream
hack/                Runtime image smoke-test helpers
ui/                  React UI embedded into the Go binary
```

Useful commands:

```bash
make ui-build
npm --prefix ui run test:ui-smoke
npm --prefix ui run test:e2e
make build
make run
make docker
go test ./...
```

You can override defaults:

```bash
IMAGE=ghcr.io/emircanagac/northscope:dev make docker
KUBECONFIG=/path/to/kubeconfig make run
```

Release tags publish versioned, multi-architecture images with SBOM and provenance attestations. Before tagging a release, keep `charts/northscope/Chart.yaml`, `charts/northscope/values.yaml`, and `CHANGELOG.md` aligned, then push a semver tag such as `v0.1.6`. The release workflow publishes the image, packages the Helm chart with a SHA-256 checksum, updates the GitHub Pages chart repository, and creates a GitHub release. NorthScope does not publish a mutable `latest` image tag.

## Community

- [Contributing](CONTRIBUTING.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Maintainers](MAINTAINERS.md)
- [Security Policy](SECURITY.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Changelog](CHANGELOG.md)

## License

Apache License 2.0. See [LICENSE](LICENSE).
