# Changelog

All notable changes to NorthScope will be documented in this file.

This project follows a lightweight changelog style inspired by Keep a Changelog. NorthScope has not reached a stable release yet.

## Unreleased

No unreleased changes.

## 0.1.4 - 2026-07-16

- Coalesced bursty Kubernetes informer events before rebuilding topology snapshots.
- Moved Node discovery to the shared informer cache instead of listing Nodes for every snapshot.
- Reduced WebSocket payloads to resources connected to supported traffic roots while preserving cluster-wide inventory counts.
- Suppressed WebSocket publication when a successful rebuild produces unchanged topology.
- Split the backend topology builder and frontend graph construction into focused modules.
- Added Chromium E2E coverage for dark mode persistence, WebSocket reconnects, pan/zoom stability, and card overflow.
- Stabilized live topology updates by preserving measured React Flow state and rendering edge labels without SVG text remeasurement flicker.
- Added Helm lint/render and browser E2E checks to pull request and main branch CI.

## 0.1.3 - 2026-07-13

- Added dark mode for the main UI, route list, topology cards, graph canvas, and React Flow controls.
- Improved topology viewport stability by avoiding graph remounts and repeated automatic fit-to-view during live updates.

## 0.1.2 - 2026-07-09

- Added troubleshooting documentation for readiness, RBAC, route diagnostics, endpoints, controller inference, optional CRDs, and metrics.
- Added Prometheus-compatible `/metrics` endpoint for readiness, snapshot version, topology size, build counts, build errors, build duration, and websocket clients.
- Added production access guidance for TLS, edge authentication, and NetworkPolicy usage.
- Added reliability validation for Kubernetes client QPS/Burst defaults, watcher cancellation, snapshot build failures, and large topology builds.
- Added topology regression coverage for the same host appearing in multiple Ingress objects.
- Strengthened topology regression coverage for named Ingress backend ports and missing Service port diagnostics.
- Added topology regression coverage for NodePort and LoadBalancer ingress controller Services.
- Added legacy core Endpoints fallback and ExternalName Service visualization for backend topology.
- Added regression coverage for common nginx, Traefik, HAProxy, and F5-related topology paths plus missing Service and zero-ready Pod diagnostics.
- Added UI smoke coverage for All namespaces, route search, host route selection, and Simple/Expanded layout mode.
- Improved dense route list and narrow-sidebar behavior for long ingress, host, and backend labels.
- RBAC was tightened by removing unused core Endpoints permissions, and the security policy now documents the exact read-only permission surface.
- Helm chart now supports an optional PodDisruptionBudget for multi-replica installations.
- Helm chart now supports an optional NetworkPolicy for production installations.
- Release workflow now publishes versioned container images and Helm charts from semver tags instead of a mutable `latest` image.
- Helm ingress template now defaults to `/` with `Prefix` when only `ingress.hosts[0].host` is set from the CLI.
- Pre-beta validation of Ingress topology visualization.
- Helm chart as the only supported installation path.
- Ingress-object sidebar grouping with nested hosts and paths.
- Simple and Expanded topology modes with the same graph layout model.
- Read-only Kubernetes watcher and WebSocket topology stream.
- Demo topology with multiple controllers, multiple Ingress objects, healthy routes, broken routes, and selector-less external endpoint examples.
- Optional Gateway API and F5 CIS discovery with reduced Kubernetes client log noise.
