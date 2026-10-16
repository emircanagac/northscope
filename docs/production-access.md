# Production Access

NorthScope exposes Kubernetes topology data, including internal hostnames, Services, Pods, Nodes, and endpoint addresses. Keep it behind trusted internal access controls.

The Helm chart enables a built-in sign-in by default. In production, combine it with:

- an internal Ingress with TLS
- optionally, authentication at the ingress controller, identity proxy, VPN, or zero-trust access layer when you need per-user identities
- a NetworkPolicy and a private ingress or load balancer
- repository and cluster access only for operators who are allowed to inspect topology metadata

## Built-in Sign-in

NorthScope asks for a username and password before it shows the UI, API, or WebSocket stream. The username defaults to `admin`. On first install the chart generates a random 24-character password and a session key in the Secret `<release>-auth` (`northscope-auth` for a release named `northscope`). Read them with:

```bash
kubectl -n northscope get secret northscope-auth -o jsonpath='{.data.username}' | base64 -d; echo
kubectl -n northscope get secret northscope-auth -o jsonpath='{.data.password}' | base64 -d; echo
```

`helm upgrade` reuses the stored password and session key through Helm `lookup`, so upgrades keep credentials and signed-in sessions. To choose a password, set `auth.password` (prefer `--set-file` or a values file that is not committed). Changing the password rolls the Pod and signs every browser out.

### helm template, Argo CD, and Flux

`lookup` returns nothing when Helm cannot query the cluster, which is the case for `helm template`, `helm install --dry-run`, and GitOps tools that render charts offline. Each render would then generate a new password and session key. Create the Secret yourself, for example with an external secret manager or Sealed Secrets, and point the chart at it:

```bash
kubectl -n northscope create secret generic northscope-login \
  --from-literal=username=admin \
  --from-literal=password="$(openssl rand -base64 24)" \
  --from-literal=session-key="$(openssl rand -hex 32)"
```

```yaml
auth:
  existingSecret: northscope-login
```

The Secret must contain `username`, `password`, and `session-key` (at least 32 characters). NorthScope reads them at startup, so restart it after rotating the Secret:

```bash
kubectl -n northscope rollout restart deploy/northscope
```

### Options

| Value | Default | Description |
| --- | --- | --- |
| `auth.enabled` | `true` | Require a sign-in. `false` serves the UI and API to anyone who can reach the Service. |
| `auth.username` | `admin` | Login name for the chart-managed Secret. |
| `auth.password` | `""` | Password for the chart-managed Secret. Empty generates one on install and keeps it on upgrade. |
| `auth.existingSecret` | `""` | Use this Secret instead of creating one. |
| `auth.cookieSecure` | `auto` | Session cookie `Secure` attribute: `auto` (HTTPS requests or `X-Forwarded-Proto: https`), `true`, or `false`. |
| `auth.protectMetrics` | `false` | Require a session for `/metrics`. Prometheus cannot sign in, so leave this off when Prometheus scrapes NorthScope. |

Sessions last 12 hours. Pass `-auth-session-ttl` through `extraArgs` to change that, for example `extraArgs: ["-auth-session-ttl=8h"]`. Behind a TLS-terminating ingress controller that sets `X-Forwarded-Proto`, `auto` already marks the cookie `Secure`; set `auth.cookieSecure=true` when browsers only ever use HTTPS.

Failed sign-ins are throttled per client address. NorthScope ignores forwarded headers for this, so behind an ingress controller every client shares the controller's budget of 5 failed attempts plus one every 30 seconds.

To turn the built-in sign-in off, for example because an identity-aware proxy already authenticates every request, set `auth.enabled=false`. NorthScope logs a warning at startup when it runs without authentication.

## Ingress With TLS

The chart creates an Ingress by default for the placeholder host `northscope.example.com`. Set your own host, and add a TLS Secret (for example from cert-manager) so the sign-in password and session cookie are encrypted:

```yaml
ingress:
  className: nginx
  hosts:
    - host: northscope.internal.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: northscope-tls
      hosts:
        - northscope.internal.example.com
```

```bash
helm upgrade --install northscope northscope/northscope \
  --namespace northscope --create-namespace -f values.yaml
```

Leave `className` empty to use the cluster's default IngressClass. To skip the Ingress, for example on a cluster without an ingress controller, set `ingress.enabled=false` and use `kubectl -n northscope port-forward svc/northscope 18080:80`.

## Authentication At The Edge

Edge authentication adds per-user identity, SSO, and audit logs on top of the built-in sign-in, or replaces it when you set `auth.enabled=false`. Common options are:

- an ingress-controller auth feature
- an identity-aware proxy such as oauth2-proxy or a platform gateway
- corporate VPN, SSO gateway, or zero-trust access product

Example with NGINX basic-auth annotations:

```yaml
ingress:
  enabled: true
  className: nginx
  annotations:
    nginx.ingress.kubernetes.io/auth-type: basic
    nginx.ingress.kubernetes.io/auth-secret: northscope-basic-auth
    nginx.ingress.kubernetes.io/auth-realm: "Authentication Required"
  hosts:
    - host: northscope.internal.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: northscope-tls
      hosts:
        - northscope.internal.example.com
```

The `northscope-basic-auth` Secret is controller-specific and must be created according to the ingress controller's documentation.

## NetworkPolicy

The chart can create an optional NetworkPolicy:

```yaml
networkPolicy:
  enabled: true
```

By default, the policy allows inbound traffic only to NorthScope's HTTP port and leaves egress unrestricted so the watcher can continue reading the Kubernetes API. Tighten `networkPolicy.ingress.from` and `networkPolicy.egress.rules` for your cluster.

## Limit The Watch Scope

Cluster-wide discovery is the default. If operators only need one application namespace, reduce the namespaced watch and RBAC surface:

```yaml
watchNamespace: apps

discovery:
  gatewayAPI:
    enabled: true
  f5:
    enabled: false
```

This creates a Role and RoleBinding in `apps` for namespaced traffic resources. NorthScope retains read-only cluster permissions for Nodes, IngressClasses, and GatewayClasses because those resources are cluster-scoped. Disabling an optional discovery provider also removes its optional runtime watches and RBAC rules.

## Capacity Guidance

The chart defaults are a starting point, not a universal production sizing target. Watch `northscope_snapshot_nodes`, `northscope_snapshot_edges`, `northscope_snapshot_build_duration_seconds`, process CPU, and memory under your real route count.

- Prefer `watchNamespace` when one team or application boundary is sufficient.
- Increase CPU and memory requests before raising limits on large clusters.
- Keep event coalescing enabled; unchanged snapshots are not sent to browsers.
- Each replica maintains its own informer cache and WebSocket clients. Additional replicas improve availability but also duplicate Kubernetes API watch and snapshot-build work.
- NorthScope is pre-beta and does not yet publish a certified maximum cluster size. Validate representative route and endpoint counts before production rollout.

## Operational Notes

- Use HTTPS for browser access.
- Keep `/metrics` behind the same trusted network or scraping boundary as the UI, or set `auth.protectMetrics=true` when nothing scrapes it. `/api/status` requires a session when the built-in sign-in is enabled.
- Keep NorthScope internal unless you have a deliberate public access control layer.
- Review `SECURITY.md` before enabling access for a production cluster.
- Prefer version-pinned installs for production change control.
- NorthScope status labels are configuration-derived diagnostics, not active reachability checks.
