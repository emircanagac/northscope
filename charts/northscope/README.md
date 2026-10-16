# NorthScope

Read-only Kubernetes traffic map: see how every host enters your cluster, from load balancer and ingress controller to Pods.

![NorthScope showing the route for a host from the load balancer to its Pods](https://raw.githubusercontent.com/emircanagac/northscope/main/docs/assets/northscope-light.png)

NorthScope follows each host through its load balancer and NodePorts, the ingress controller, Ingress or Gateway API routes, Services, and EndpointSlices to Pods and Nodes. It flags missing Services, wrong backend ports, Pods that are not endpoints, and backends without ready endpoints. It only reads Kubernetes configuration; it does not capture traffic or probe connectivity.

NorthScope is an early preview. Feedback and [bug reports](https://github.com/emircanagac/northscope/issues) are welcome.

## Requirements

- Kubernetes 1.20 or newer
- Helm 3
- An ingress controller for the default Ingress (or set `ingress.enabled=false`)

## Install

Set your host, and ideally a TLS Secret, in a values file:

```yaml
# values.yaml
ingress:
  hosts:
    - host: northscope.your-domain.com
  tls:
    - secretName: northscope-tls
      hosts:
        - northscope.your-domain.com
```

```bash
helm repo add northscope https://emircanagac.github.io/northscope
helm repo update
helm upgrade --install northscope northscope/northscope \
  --namespace northscope --create-namespace -f values.yaml
```

Open the host and sign in as `admin` with the generated password:

```bash
kubectl -n northscope get secret northscope-auth -o jsonpath='{.data.password}' | base64 -d; echo
```

Without an ingress controller, set `ingress.enabled=false` and use `kubectl -n northscope port-forward svc/northscope 18080:80`.

## Configuration

| Key | Default | Description |
| --- | --- | --- |
| `ingress.enabled` | `true` | Expose NorthScope through an Ingress. |
| `ingress.className` | `""` | IngressClass to use; empty uses the cluster default. |
| `ingress.hosts[0].host` | `northscope.example.com` | Host name of the UI. Change this. |
| `ingress.tls` | `[]` | TLS configuration, recommended so the sign-in is encrypted. |
| `auth.enabled` | `true` | Require a username and password for the UI, API, and WebSocket. |
| `auth.username` | `admin` | Sign-in user name. |
| `auth.password` | `""` | Sign-in password; empty generates one on install and keeps it on upgrade. |
| `auth.existingSecret` | `""` | Secret with `username`, `password`, and `session-key`; use it with `helm template`, Argo CD, and Flux. |
| `auth.cookieSecure` | `auto` | Session cookie `Secure` attribute: `auto`, `true`, or `false`. |
| `auth.protectMetrics` | `false` | Require a session for `/metrics`. |
| `watchNamespace` | `""` | Watch one namespace instead of all namespaces, with namespaced RBAC. |
| `discovery.gatewayAPI.enabled` | `true` | Discover Gateway API resources when their CRDs are installed. |
| `discovery.f5.enabled` | `true` | Discover F5 CIS resources when their CRDs are installed. |
| `service.type` | `ClusterIP` | Service type. |
| `networkPolicy.enabled` | `false` | Create a NetworkPolicy for the NorthScope Pod. |
| `podDisruptionBudget.enabled` | `false` | Create a PodDisruptionBudget. |
| `resources` | 25m/64Mi requests, 250m/256Mi limits | Raise memory on large clusters. |

See [values.yaml](https://github.com/emircanagac/northscope/blob/main/charts/northscope/values.yaml) for every option. The chart validates values against `values.schema.json`.

## Security

NorthScope uses only `get`, `list`, and `watch`, and never reads Secrets or Pod logs through the Kubernetes API. Its own sign-in credentials are mounted into the Pod as a file. The default Pod satisfies the Pod Security Standards `restricted` profile. See the [security policy](https://github.com/emircanagac/northscope/blob/main/SECURITY.md).

## Uninstall

```bash
helm uninstall northscope -n northscope
```

## Documentation

- [Production access](https://github.com/emircanagac/northscope/blob/main/docs/production-access.md)
- [Troubleshooting](https://github.com/emircanagac/northscope/blob/main/docs/troubleshooting.md)
- [Changelog](https://github.com/emircanagac/northscope/blob/main/CHANGELOG.md)
- [Source code](https://github.com/emircanagac/northscope)
