# Changelog

All notable changes to NorthScope will be documented in this file.

This project follows a lightweight changelog style inspired by Keep a Changelog. NorthScope is an early preview: real-cluster validation for scale and platform-specific installs is still in progress.

## Unreleased

- Linked Ingresses without a resolvable IngressClass to the controller whose Service load balancer address appears in the Ingress status, so controllers that never created an IngressClass (such as Traefik on k3s 1.21) show their LoadBalancer and NodePort entry path instead of a missing-controller warning.
- Routes now warn when running Pods match a Service selector but are missing from its EndpointSlices or Endpoints (for example a named `targetPort` the Pods do not expose), because those Pods receive no traffic.
- ExternalName Services without ports no longer get a false "no matching port" error.
- With `watchNamespace` set, Gateway API references to Gateways or Services in other namespaces are reported as outside the watch scope instead of as missing.

## 1.0.0 - 2026-10-16

First public release.

- Read-only topology for Ingress, Gateway API (GatewayClass, Gateway, HTTP/gRPC/TLS/TCP/UDP routes, ReferenceGrant), and F5 CIS (VirtualServer, TransportServer, IngressLink), traced from load balancers, NodePorts, and ingress controllers through Services and EndpointSlices or Endpoints to Pods and Nodes. IngressClass and GatewayClass controllers with the same name are kept apart.
- Route diagnostics for missing Services, wrong backend ports, unsupported backends, selectors without Pods, Pods or endpoints that are not ready, and unresolved IngressClass ownership.
- Kubernetes 1.20+ support: the served EndpointSlice API is detected at startup (`discovery.k8s.io/v1`, `v1beta1` on 1.20, or legacy Endpoints only), and classes referenced only through the legacy `kubernetes.io/ingress.class` annotation get an inferred controller.
- Web UI with host and route search across namespaces, Simple and Expanded layouts, dark mode, and live WebSocket updates. Unchanged cluster state is not republished, and after a backend restart or failover the UI switches to the new process's topology immediately.
- Watch health reporting: per-resource list/watch status through `/api/status`, `/readyz?verbose`, the snapshot `health` section, and metrics, with a `Degraded` banner and snapshot age in the UI when part of the cache may be stale.
- Built-in username/password sign-in, enabled by default: the UI, `/api/*`, and the WebSocket require a signed session cookie, failed sign-ins are throttled, and responses carry a restrictive Content-Security-Policy. The chart generates a `<release>-auth` Secret with a random password, or uses `auth.existingSecret`.
- Helm chart with an Ingress enabled by default (set your host and, ideally, a TLS Secret), read-only RBAC, optional namespace-scoped watches, optional Gateway API and F5 discovery, NetworkPolicy and PodDisruptionBudget options, and a default Pod that satisfies the Pod Security Standards `restricted` profile without a fixed UID.
- `/healthz`, `/readyz`, `/metrics`, `/api/version`, `/api/session`, and `/api/status` endpoints.
- Helm chart repository landing page with install steps, requirements, and checksum verification.
- Multi-architecture (linux/amd64, linux/arm64) container images with SBOM and provenance attestations, and Helm chart packages with SHA-256 checksums.
