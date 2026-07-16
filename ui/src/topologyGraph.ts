import type { TopologyEdge, TopologyNode } from './hooks/useTopologyStream';
import { isControllerNode, isIngressNode, nodeDisplayName } from './topologyView';
import {
  ingressHostLaneId,
  layoutTrafficPath,
  safeVisualId,
  severityRank,
  type RouteItem,
  type TopologyMode,
} from './trafficGraph';

const TRAFFIC_NODE_ID_PREFIX = 'visual:f5-edge';
const edgeLabelStyle = {
  fill: '#334155',
  fontSize: 10,
  fontWeight: 700,
} as const;
const edgeLabelBgStyle = {
  fill: '#f8fafc',
  fillOpacity: 0.96,
} as const;
const edgeStyle = {
  stroke: '#94a3b8',
  strokeWidth: 2,
} as const;
interface NamespaceTrafficGraph {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
  routes: RouteItem[];
}

interface HostRouteRecord {
  route: TopologyNode;
  serviceEdge?: TopologyEdge;
  service?: TopologyNode;
  displayService: TopologyNode;
  host: string;
  hostLaneId: string;
}

function kindOf(node?: TopologyNode): string {
  return String(node?.data.kind ?? '').toLowerCase();
}

function edgeKind(edge: TopologyEdge): string {
  return String(edge.data?.kind ?? '').toLowerCase();
}

function edgeLabel(kind: string): string {
  switch (kind.toLowerCase()) {
    case 'traffic':
      return 'enters';
    case 'forwards':
      return 'forwards';
    case 'controls':
      return 'watches';
    case 'defines':
      return 'matches';
    case 'routes':
      return 'routes';
    case 'selects':
      return 'selects';
    case 'endpointslice':
    case 'endpoint':
    case 'externalname':
      return 'targets';
    case 'runs_on':
    case 'hosts':
      return 'runs on';
    case 'missing':
      return 'missing';
    default:
      return kind;
  }
}

function normalizeEdge(edge: TopologyEdge): TopologyEdge {
  return {
    ...edge,
    type: 'northscopeStep',
    labelStyle: edgeLabelStyle,
    labelBgStyle: edgeLabelBgStyle,
    labelBgPadding: [6, 4],
    labelBgBorderRadius: 4,
    style: {
      ...edgeStyle,
      ...(edge.style ?? {}),
    },
  };
}
function displayEdge(edge: TopologyEdge): TopologyEdge {
  const kind = edgeKind(edge);
  return normalizeEdge({
    ...edge,
    label: edgeLabel(kind),
  });
}

function syntheticEdge(source: string, target: string, kind: string, label: string): TopologyEdge {
  return normalizeEdge({
    id: `${source}->${target}:${kind}`,
    source,
    target,
    label: label || edgeLabel(kind),
    animated: kind === 'traffic',
    data: { kind },
  });
}

function namespaceTrafficNode(namespace: string): TopologyNode {
  return {
    id: `${TRAFFIC_NODE_ID_PREFIX}:${namespace}`,
    type: 'northscopeNode',
    position: { x: 0, y: 0 },
    data: {
      label: 'F5 / LB',
      kind: 'ExternalEdge',
      namespace,
      name: 'F5 / LB',
      status: 'Assumed entry',
      properties: {
        role: 'Traffic entry point',
      },
    },
  };
}

function syntheticControllerNode(namespace: string): TopologyNode {
  return {
    id: `visual:controller:${namespace}`,
    type: 'northscopeNode',
    position: { x: 260, y: 0 },
    data: {
      label: `${namespace}/ingress-controller`,
      kind: 'Controller',
      namespace,
      name: 'ingress-controller',
      status: 'Inferred',
      properties: {
        role: 'fallback controller',
      },
    },
  };
}

function routeHost(route: TopologyNode): string {
  if (route.data.properties?.defaultBackend === 'true') {
    return 'default backend';
  }
  const rawHost = route.data.properties?.host || '*';
  return rawHost.trim().split(/\s+/)[0].split('/')[0] || '*';
}

function routePath(route: TopologyNode): string {
  if (route.data.properties?.defaultBackend === 'true') {
    return 'default';
  }
  return route.data.properties?.path || '/';
}

function ingressRouteLabel(ingress: TopologyNode): string {
  return String(ingress.data.name || ingress.data.label || nodeDisplayName(ingress));
}

function routeItemFromNode(route: TopologyNode, ingress: TopologyNode, hostLaneId: string, service?: TopologyNode): RouteItem {
  const props = route.data.properties ?? {};
  const host = routeHost(route);
  const path = routePath(route);
  return {
    id: route.id,
    ingressId: ingress.id,
    serviceId: service?.id ?? '',
    namespace: String(ingress.data.namespace ?? route.data.namespace ?? ''),
    name: String(route.data.label ?? route.data.name),
    host,
    path,
    hostLaneId,
    ingress: ingressRouteLabel(ingress),
    backend: props.backend ?? (service ? nodeDisplayName(service) : 'missing service'),
    status: String(route.data.status ?? 'Unknown'),
    severity: props.severity ?? 'unknown',
  };
}

function syntheticMissingServiceNode(namespace: string, route: TopologyNode): TopologyNode {
  const serviceName = route.data.properties?.service ?? 'missing-service';
  return {
    id: `visual:missing-service:${namespace}:${route.id}`,
    type: 'northscopeNode',
    position: { x: 0, y: 0 },
    data: {
      label: serviceName,
      kind: 'Service',
      namespace,
      name: serviceName,
      status: 'Missing',
      properties: {
        role: 'missing backend service',
      },
    },
  };
}

function laneNode(node: TopologyNode, laneId: string): TopologyNode {
  return {
    ...node,
    id: `${node.id}:lane:${laneId}`,
    position: { ...node.position },
    data: {
      ...node.data,
      properties: {
        ...(node.data.properties ?? {}),
        visualLane: laneId,
      },
    },
  };
}

function laneIngressNode(ingress: TopologyNode, laneId: string, host: string, routes: HostRouteRecord[]): TopologyNode {
  const ingressName = String(ingress.data.name || ingress.data.label || nodeDisplayName(ingress));
  const paths = Array.from(new Set(routes.map((record) => routePath(record.route)))).join(', ');
  return {
    ...laneNode(ingress, laneId),
    data: {
      ...ingress.data,
      label: host,
      name: host,
      properties: {
        ...(ingress.data.properties ?? {}),
        visualLane: laneId,
        hosts: host,
        host,
        ingressName,
        selectedHost: host,
        selectedPaths: paths,
      },
    },
  };
}

function syntheticHostNode(namespace: string, host: string): TopologyNode {
  return {
    id: `visual:dns:${namespace}:${safeVisualId(host)}`,
    type: 'northscopeNode',
    position: { x: 0, y: 0 },
    data: {
      label: host,
      kind: 'DNS',
      name: host,
      status: 'Host',
      properties: {
        role: 'Ingress host',
      },
    },
  };
}

function isReadyPod(node?: TopologyNode): boolean {
  const status = String(node?.data.status ?? node?.data.phase ?? '').toLowerCase();
  if (status.includes('notready')) {
    return false;
  }
  return status.includes('ready') || status === 'running';
}

function syntheticPodSummaryNode(namespace: string, route: TopologyNode, pods: TopologyNode[]): TopologyNode {
  const readyPods = pods.filter(isReadyPod).length;
  const totalPods = pods.length;
  const status = totalPods === 0 ? 'No pods' : `${readyPods} ready / ${totalPods} pods`;
  return {
    id: `visual:pod-summary:${route.id}`,
    type: 'northscopeNode',
    position: { x: 0, y: 0 },
    data: {
      label: status,
      kind: 'PodGroup',
      namespace,
      name: status,
      status,
      properties: {
        summary: totalPods === 0 ? 'No matching pods observed' : pods.map((pod) => pod.data.name).join(', '),
      },
    },
  };
}

function laneEdge(source: TopologyNode, target: TopologyNode, kind: string): TopologyEdge {
  return syntheticEdge(source.id, target.id, kind, edgeLabel(kind));
}

function nodePortRole(node?: TopologyNode): string {
  const text = [
    node?.data.name,
    node?.data.label,
    node?.data.properties?.servicePort,
    node?.data.properties?.nodePort,
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();

  if (/\bhttps\b/.test(text) || /\b443\b/.test(text)) return 'https';
  if (/\bhttp\b/.test(text) || /\b80\b/.test(text)) return 'http';
  return 'other';
}

function pickControllerNodePorts(controllerId: string, nodesById: Map<string, TopologyNode>, edges: TopologyEdge[]): TopologyNode[] {
  const candidates = edges
    .filter((edge) => edge.target === controllerId && edgeKind(edge) === 'forwards')
    .map((edge) => nodesById.get(edge.source))
    .filter((node): node is TopologyNode => kindOf(node) === 'nodeport')
    .sort((left, right) => nodeDisplayName(left).localeCompare(nodeDisplayName(right)));

  if (candidates.length <= 1) {
    return candidates;
  }

  const byRole = new Map<string, TopologyNode>();
  for (const node of candidates) {
    const role = nodePortRole(node);
    if (!byRole.has(role)) {
      byRole.set(role, node);
    }
  }

  const picked = ['http', 'https'].map((role) => byRole.get(role)).filter((node): node is TopologyNode => Boolean(node));
  if (picked.length > 0) {
    return picked;
  }

  return [candidates[0]];
}

export function buildNamespaceTrafficGraph(
  namespace: string,
  nodes: TopologyNode[],
  edges: TopologyEdge[],
  mode: TopologyMode,
): NamespaceTrafficGraph {
  const nodeById = new Map(nodes.map((node) => [node.id, node]));
  const namespaceMatches = (value?: string) => !namespace || value === namespace;
  const ingressNodes = nodes.filter((node) => isIngressNode(node) && namespaceMatches(node.data.namespace));
  if (ingressNodes.length === 0) {
    return { nodes: [], edges: [], routes: [] };
  }

  const graphNodes = new Map<string, TopologyNode>();
  const graphEdges = new Map<string, TopologyEdge>();
  const routeItems: RouteItem[] = [];

  const addNode = (node?: TopologyNode) => {
    if (node) {
      graphNodes.set(node.id, node);
    }
  };
  const addEdge = (edge: TopologyEdge) => {
    graphEdges.set(edge.id, displayEdge(edge));
  };
  const podsForService = (service: TopologyNode): TopologyNode[] =>
    edges
      .filter((edge) => edge.source === service.id && ['selects', 'endpointslice', 'endpoint', 'externalname'].includes(edgeKind(edge)))
      .map((edge) => nodeById.get(edge.target))
      .filter((node): node is TopologyNode => {
        if (!node) {
          return false;
        }
        return kindOf(node) === 'pod' && node.data.namespace === service.data.namespace;
      });
  const externalEndpointsForService = (service: TopologyNode): TopologyNode[] =>
    edges
      .filter((edge) => edge.source === service.id && ['endpointslice', 'endpoint', 'externalname'].includes(edgeKind(edge)))
      .map((edge) => nodeById.get(edge.target))
      .filter((node): node is TopologyNode => ['endpointslice', 'endpoint'].includes(kindOf(node)) && node?.data.namespace === service.data.namespace);

  for (const ingress of ingressNodes) {
    const ingressNamespace = String(ingress.data.namespace ?? '');
    const controllerEdges = edges.filter((edge) => edge.target === ingress.id && edgeKind(edge) === 'controls');

    const routeEdges = edges.filter((edge) => edge.source === ingress.id && edgeKind(edge) === 'defines');
    const routeRecords: HostRouteRecord[] = [];
    for (const routeEdge of routeEdges) {
      const route = nodeById.get(routeEdge.target);
      if (kindOf(route) !== 'route' || route?.data.namespace !== ingressNamespace) {
        continue;
      }

      const serviceEdge = edges.find((edge) => edge.source === route.id && edgeKind(edge) === 'routes');
      const service = serviceEdge ? nodeById.get(serviceEdge.target) : undefined;
      const displayService = service ?? syntheticMissingServiceNode(ingressNamespace, route);
      const host = routeHost(route);
      const routeHostLaneId = ingressHostLaneId(ingressNamespace, ingress, host);
      routeItems.push(routeItemFromNode(route, ingress, routeHostLaneId, displayService));
      routeRecords.push({ route, serviceEdge, service, displayService, host, hostLaneId: routeHostLaneId });
    }

    const recordsByHostLane = new Map<string, HostRouteRecord[]>();
    for (const record of routeRecords) {
      recordsByHostLane.set(record.hostLaneId, [...(recordsByHostLane.get(record.hostLaneId) ?? []), record]);
    }

    for (const [routeHostLaneId, hostRecords] of recordsByHostLane.entries()) {
      const host = hostRecords[0].host;
      const laneExternal = laneNode(namespaceTrafficNode(ingressNamespace), routeHostLaneId);
      const laneIngress = laneIngressNode(ingress, routeHostLaneId, host, hostRecords);

      addNode(laneExternal);
      addNode(laneIngress);

      const controllers = controllerEdges
        .map((edge) => nodeById.get(edge.source))
        .filter((node): node is TopologyNode => (node ? isControllerNode(node) : false));
      const controllersForLane = controllers.length > 0 ? controllers : [syntheticControllerNode(ingressNamespace)];

      for (const controller of controllersForLane) {
        const laneController = laneNode(controller, routeHostLaneId);
        addNode(laneController);
        const nodePorts = pickControllerNodePorts(controller.id, nodeById, edges);
        if (nodePorts.length > 0) {
          for (const nodePort of nodePorts) {
            const laneNodePort = laneNode(nodePort, routeHostLaneId);
            addNode(laneNodePort);
            addEdge(laneEdge(laneExternal, laneNodePort, 'traffic'));
            addEdge(laneEdge(laneNodePort, laneController, 'forwards'));
          }
        } else {
          addEdge(laneEdge(laneExternal, laneController, 'traffic'));
        }
        addEdge(laneEdge(laneController, laneIngress, 'controls'));
      }

      if (mode === 'expanded') {
        const laneHost = laneNode(syntheticHostNode(ingressNamespace, host), routeHostLaneId);
        addNode(laneHost);
        addEdge(laneEdge(laneIngress, laneHost, 'defines'));

        for (const record of hostRecords) {
          const laneRoute = laneNode(record.route, record.route.id);
          const laneService = laneNode(record.displayService, record.route.id);

          addNode(laneRoute);
          addNode(laneService);
          addEdge(laneEdge(laneHost, laneRoute, 'defines'));
          addEdge(laneEdge(laneRoute, laneService, record.serviceEdge && record.service ? 'routes' : 'missing'));

          const podEdges = edges.filter(
            (edge) => edge.source === record.displayService.id && ['selects', 'endpointslice', 'endpoint', 'externalname'].includes(edgeKind(edge)),
          );
          for (const podEdge of podEdges) {
            const backend = nodeById.get(podEdge.target);
            if (!backend || backend.data.namespace !== record.displayService.data.namespace) {
              continue;
            }
            if (kindOf(backend) === 'endpointslice' || kindOf(backend) === 'endpoint') {
              const laneEndpoint = laneNode(backend, record.route.id);
              addNode(laneEndpoint);
              addEdge(laneEdge(laneService, laneEndpoint, edgeKind(podEdge)));
              continue;
            }
            if (kindOf(backend) !== 'pod') {
              continue;
            }
            const lanePod = laneNode(backend, record.route.id);
            addNode(lanePod);
            addEdge(laneEdge(laneService, lanePod, edgeKind(podEdge)));

            const nodeHostEdges = edges.filter((edge) => edge.target === backend.id && edgeKind(edge) === 'hosts');
            for (const nodeHostEdge of nodeHostEdges) {
              const node = nodeById.get(nodeHostEdge.source);
              if (!node || kindOf(node) !== 'node') {
                continue;
              }
              const laneKubeNode = laneNode(node, record.route.id);
              addNode(laneKubeNode);
              addEdge(laneEdge(lanePod, laneKubeNode, 'runs_on'));
            }
          }
        }
      } else {
        for (const record of hostRecords) {
          const laneService = laneNode(record.displayService, record.route.id);
          addNode(laneService);
          addEdge(laneEdge(laneIngress, laneService, record.serviceEdge && record.service ? 'routes' : 'missing'));

          if (record.service) {
            const externalEndpoints = externalEndpointsForService(record.displayService);
            if (externalEndpoints.length > 0) {
              for (const endpoint of externalEndpoints) {
                const laneEndpoint = laneNode(endpoint, record.route.id);
                addNode(laneEndpoint);
                addEdge(laneEdge(laneService, laneEndpoint, 'endpoint'));
              }
            } else {
              const lanePodSummary = laneNode(
                syntheticPodSummaryNode(ingressNamespace, record.route, podsForService(record.displayService)),
                record.route.id,
              );
              addNode(lanePodSummary);
              addEdge(laneEdge(laneService, lanePodSummary, 'selects'));
            }
          }
        }
      }
    }

    if (routeEdges.length === 0) {
      const serviceEdges = edges.filter((edge) => edge.source === ingress.id && edgeKind(edge) === 'routes');
      for (const serviceEdge of serviceEdges) {
        const service = nodeById.get(serviceEdge.target);
        if (kindOf(service) !== 'service' || service?.data.namespace !== ingressNamespace) {
          continue;
        }
        addNode(service);
        addEdge(serviceEdge);
      }
    }
  }

  return {
    nodes: layoutTrafficPath(Array.from(graphNodes.values()), mode),
    edges: Array.from(graphEdges.values()),
    routes: routeItems.sort((left, right) => {
      const severity = severityRank(left.severity) - severityRank(right.severity);
      if (severity !== 0) return severity;
      const ingress = left.ingress.localeCompare(right.ingress);
      if (ingress !== 0) return ingress;
      return left.name.localeCompare(right.name);
    }),
  };
}
