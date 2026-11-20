export interface EmptyStateInput {
  hasSnapshot: boolean;
  connected: boolean;
  hasTopology: boolean;
  hasRoutes: boolean;
  hasFilteredRoutes: boolean;
  routeSelected: boolean;
  namespace: string;
  unsynced: string[];
}

export interface EmptyStateText {
  title: string;
  description: string;
}

// emptyStateText explains an empty canvas: still connecting, waiting for
// caches, nothing selected yet, or no routes to show.
export function emptyStateText(input: EmptyStateInput): EmptyStateText {
  if (!input.hasSnapshot) {
    return {
      title: input.connected ? 'Connected; waiting for first Kubernetes config snapshot' : 'Waiting for topology stream',
      description:
        input.unsynced.length > 0
          ? `Waiting for Kubernetes caches: ${input.unsynced.join(', ')}. Check pod logs, /api/status, and read-only RBAC for these resources.`
          : 'If this stays here, check pod logs and read-only RBAC for ingresses, services, pods, endpointslices, endpoints, and ingressclasses.',
    };
  }
  if (input.hasRoutes && input.hasFilteredRoutes && !input.routeSelected) {
    return {
      title: 'Select a host route to draw topology',
      description: 'Search by host, ingress, service, path, or namespace, then select a host route from the list.',
    };
  }
  if (input.hasTopology) {
    return input.namespace
      ? {
          title: 'No traffic routes found in this namespace',
          description: 'NorthScope needs an Ingress, Gateway API route, or supported F5 resource in the selected namespace.',
        }
      : {
          title: 'No traffic routes found',
          description: 'Search by host, resource, service, path, or namespace to find a traffic route.',
        };
  }
  return {
    title: 'Snapshot received, but no supported topology objects were found',
    description: 'The cluster may be empty for watched resources, or NorthScope may not have permission to list them.',
  };
}
