# Security Policy

NorthScope is a read-only Kubernetes topology debugger. Security is especially important because the application runs inside a cluster and watches cluster resources.

## Supported Versions

NorthScope is currently pre-beta and requires Kubernetes 1.20 or newer. Security fixes target the `main` branch and the latest published container image.

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
- The UI, API, and WebSocket should require a sign-in unless an operator explicitly disables it.

Changes that weaken this model require explicit maintainer review.

## Authentication

The Helm chart enables NorthScope's built-in sign-in by default. It is a single shared login meant to keep cluster topology away from anyone who can merely reach the Service; it is not a multi-user identity system.

- Credentials come from files in the directory passed with `-auth-dir`: `username`, `password`, and `session-key`. The chart mounts them read-only from a Secret. NorthScope never reads Secrets through the Kubernetes API, and its RBAC grants no Secret access.
- Without `-auth-dir`, NorthScope runs without authentication and logs a warning at startup. With `-auth-dir` set, a missing or empty file, or a session key shorter than 32 characters, stops startup.
- Usernames and passwords are compared as SHA-256 digests in constant time. Failed sign-ins return one generic message.
- Failed sign-ins are throttled per client: 5 attempts, then one more every 30 seconds. Behind a proxy on a private, shared, or loopback address, the client is the last `X-Forwarded-For` entry (appended by the proxy itself) or `X-Real-IP`, and all clients of that proxy share a wider budget of 50 failed attempts plus one every 2 seconds, so forged forwarded addresses cannot bypass the limit.
- Sessions are stateless HMAC-SHA256-signed cookies (`HttpOnly`, `SameSite=Lax`, `Path=/`, 12 hours by default via `-auth-session-ttl`). `Secure` is set for HTTPS requests or `X-Forwarded-Proto: https`; set `-auth-cookie-secure` (`auth.cookieSecure`) to `true` to always require it. Open WebSocket streams are closed when their session expires.
- Signing out clears the cookie in that browser only. To revoke every session, change the password or the session key and restart NorthScope; the signing key is derived from both.
- Sign-in and sign-out forms reject cross-site posts. Every response sets `Content-Security-Policy` (same-origin resources only, `frame-ancestors 'none'`), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, and `Referrer-Policy: no-referrer`.
- `/healthz`, `/readyz`, and `/metrics` stay open for probes and Prometheus. `/readyz?verbose` lists resource names only. Set `-auth-protect-metrics` (`auth.protectMetrics`) to require a session for `/metrics` when nothing scrapes it.

Built-in sign-in does not replace TLS. Serve NorthScope over HTTPS whenever traffic leaves a trusted network, and use an identity-aware proxy or SSO gateway in front of it when you need per-user identities or audit trails.

## Kubernetes Permissions

The default Helm chart creates read-only cluster-wide permissions because traffic paths commonly cross namespaces and Node placement is part of the topology. Set `watchNamespace` to use a namespaced Role for namespaced resources while retaining a small ClusterRole for Nodes and class metadata.

| API group | Resources | Purpose |
| --- | --- | --- |
| core | nodes | Backend Node placement |
| core | pods, services, endpoints | Backend Pod readiness, Service routing, and legacy Endpoints fallback |
| discovery.k8s.io | endpointslices | EndpointSlice and selector-less Service resolution |
| networking.k8s.io | ingressclasses, ingresses | Ingress ownership, hosts, paths, and controller mapping |
| gateway.networking.k8s.io | gatewayclasses, gateways, grpcroutes, httproutes, tcproutes, tlsroutes, udproutes, referencegrants | Optional Gateway API topology and cross-namespace backend authorization when the CRDs are installed |
| cis.f5.com | ingresslinks, virtualservers, transportservers | Optional F5 CIS topology when the CRDs are installed |

The chart does not grant access to Secrets, ConfigMaps, Events, Pod logs, Pod exec, or resource mutation verbs such as `create`, `update`, `patch`, or `delete`.

Gateway API and F5 discovery can be disabled independently. Their optional RBAC rules and runtime watches are omitted when disabled.

If you disable chart-managed RBAC with `rbac.create=false`, provide an equivalent read-only role before starting NorthScope.

## Exposed Data

NorthScope can display internal topology metadata, including:

- hostnames and Ingress paths
- Service names and ports
- Pod names, readiness state, and Pod IPs
- Node names
- EndpointSlice and legacy Endpoints addresses, including external endpoint IPs for selector-less Services
- Gateway API and F5 CIS object names and selected routing fields when those CRDs are present

Published topology snapshots contain only resources connected to supported Ingress, Gateway API, or F5 traffic roots. In default mode, inventory counters remain cluster-wide. With `watchNamespace` set, namespaced counters and routes are limited to that namespace while Node and class metadata remain cluster-scoped.

NorthScope also exposes `/metrics` with operational counters and gauges such as readiness, snapshot size, build errors, build duration, websocket clients, and per-resource watch health. `/api/status` and the UI health banner include the latest Kubernetes API error for degraded resources, which can name the NorthScope ServiceAccount and the API server address; `/api/status` requires a session when authentication is enabled. `/metrics` stays open by default, so keep it behind the same trusted boundary as the UI or set `auth.protectMetrics`.

Route health labels are derived from Kubernetes object status, backend references, ports, readiness, and endpoint data. They are not active network probes and must not be treated as proof that an application or external load balancer is reachable.

Run NorthScope behind trusted internal access controls. Keep the built-in sign-in enabled, and if NorthScope is exposed through Ingress, configure TLS and consider an identity-aware proxy at the ingress controller or platform edge. See [Production Access](docs/production-access.md) for examples.

## Deployment Hardening

Recommended production settings:

- use a private/internal hostname
- keep `auth.enabled=true`, and enable TLS before exposing the UI outside a trusted network
- set `auth.cookieSecure=true` when every browser reaches NorthScope over HTTPS
- manage credentials with `auth.existingSecret` when the chart is rendered by `helm template` or a GitOps tool
- enable `networkPolicy` where your CNI enforces NetworkPolicy
- enable `podDisruptionBudget` when running more than one replica
- keep `securityContext` and `podSecurityContext` hardened
- pin chart versions for production changes and verify the chart checksum from the GitHub release
- verify the published container provenance attestation before promotion
