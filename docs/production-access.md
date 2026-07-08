# Production Access

NorthScope exposes Kubernetes topology data, including internal hostnames, Services, Pods, Nodes, and endpoint addresses. Keep it behind trusted internal access controls.

NorthScope does not include built-in user authentication. In production, prefer one of these patterns:

- expose NorthScope through an internal Ingress with TLS
- require authentication at the ingress controller, identity proxy, VPN, or zero-trust access layer
- restrict network access with a NetworkPolicy and private ingress/load balancer
- grant repository and cluster access only to operators who are allowed to inspect topology metadata

## Ingress With TLS

Create or provision a TLS Secret for the host, then install NorthScope with ingress enabled:

```bash
helm upgrade --install northscope northscope/northscope \
  --namespace northscope \
  --create-namespace \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.hosts[0].host=northscope.internal.example.com \
  --set ingress.tls[0].secretName=northscope-tls \
  --set ingress.tls[0].hosts[0]=northscope.internal.example.com
```

For more complex ingress settings, use a values file:

```yaml
ingress:
  enabled: true
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

## Authentication At The Edge

Authentication should be enforced before traffic reaches NorthScope. Common options are:

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

## Operational Notes

- Use HTTPS for browser access.
- Keep NorthScope internal unless you have a deliberate public access control layer.
- Review `SECURITY.md` before enabling access for a production cluster.
- Prefer version-pinned installs for production change control.
