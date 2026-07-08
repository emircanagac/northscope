# Changelog

All notable changes to NorthScope will be documented in this file.

This project follows a lightweight changelog style inspired by Keep a Changelog. NorthScope has not reached a stable release yet.

## Unreleased

- Added production access guidance for TLS, edge authentication, and NetworkPolicy usage.
- Added reliability validation for Kubernetes client QPS/Burst defaults, watcher cancellation, snapshot build failures, and large topology builds.
- Added topology regression coverage for the same host appearing in multiple Ingress objects.
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
