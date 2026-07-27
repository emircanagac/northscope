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

interface ExternalRouteRecord {
  route: TopologyNode;
  service?: TopologyNode;
  displayService: TopologyNode;
  host: string;
  path: string;
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
    case 'attaches':
      return 'attaches';
    case 'rejected':
      return 'rejected';
    case 'balances':
      return 'balances';
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

function propertyValues(value?: string): string[] {
  return String(value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean);
}

function routeSeverity(node: TopologyNode): string {
  const explicit = String(node.data.properties?.severity ?? '').toLowerCase();
  if (explicit) return explicit;
  const status = String(node.data.status ?? '').toLowerCase();
  if (status.includes('false') || status.includes('error') || status.includes('rejected')) return 'error';
  if (status.includes('pending') || status.includes('unknown')) return 'warning';
  return 'ok';
}

function routeItemFromNode(route: TopologyNode, ingress: TopologyNode, hostLaneId: string, service?: TopologyNode): RouteItem {
  const props = route.data.properties ?? {};
  const host = routeHost(route);
  const path = routePath(route);
  return {
    id: route.id,
    topologyId: route.id,
    ingressId: ingress.id,
    rootKind: 'Ingress',
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

function pickControllerNodePorts(nodesById: Map<string, TopologyNode>, incomingEdges: TopologyEdge[]): TopologyNode[] {
  const candidates = incomingEdges
    .filter((edge) => edgeKind(edge) === 'forwards')
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
  const outgoingBySource = new Map<string, TopologyEdge[]>();
  const incomingByTarget = new Map<string, TopologyEdge[]>();
  for (const edge of edges) {
    outgoingBySource.set(edge.source, [...(outgoingBySource.get(edge.source) ?? []), edge]);
    incomingByTarget.set(edge.target, [...(incomingByTarget.get(edge.target) ?? []), edge]);
  }
  const outgoingEdges = (nodeId: string, kinds?: string[]): TopologyEdge[] =>
    (outgoingBySource.get(nodeId) ?? []).filter((edge) => !kinds || kinds.includes(edgeKind(edge)));
  const incomingEdges = (nodeId: string, kinds?: string[]): TopologyEdge[] =>
    (incomingByTarget.get(nodeId) ?? []).filter((edge) => !kinds || kinds.includes(edgeKind(edge)));
  const namespaceMatches = (value?: string) => !namespace || value === namespace;
  const ingressNodes = nodes.filter((node) => isIngressNode(node) && namespaceMatches(node.data.namespace));
  const gatewayNodes = nodes.filter((node) => kindOf(node) === 'gateway' && namespaceMatches(node.data.namespace));
  const f5Nodes = nodes.filter(
    (node) =>
      kindOf(node) === 'loadbalancer' &&
      String(node.data.properties?.provider ?? '').toLowerCase() === 'f5' &&
      namespaceMatches(node.data.namespace),
  );
  if (ingressNodes.length === 0 && gatewayNodes.length === 0 && f5Nodes.length === 0) {
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
    outgoingEdges(service.id, ['selects', 'endpointslice', 'endpoint', 'externalname'])
      .map((edge) => nodeById.get(edge.target))
      .filter((node): node is TopologyNode => {
        if (!node) {
          return false;
        }
        return kindOf(node) === 'pod' && node.data.namespace === service.data.namespace;
      });
  const externalEndpointsForService = (service: TopologyNode): TopologyNode[] =>
    outgoingEdges(service.id, ['endpointslice', 'endpoint', 'externalname'])
      .map((edge) => nodeById.get(edge.target))
      .filter((node): node is TopologyNode => ['endpointslice', 'endpoint'].includes(kindOf(node)) && node?.data.namespace === service.data.namespace);
  const addServiceBranch = (
    source: TopologyNode,
    service: TopologyNode,
    laneId: string,
    summarySource: TopologyNode,
    relationKind = 'routes',
  ) => {
    const laneService = laneNode(service, laneId);
    addNode(laneService);
    addEdge(laneEdge(source, laneService, relationKind));

    const backendEdges = outgoingEdges(service.id, ['selects', 'endpointslice', 'endpoint', 'externalname']);
    if (mode === 'expanded') {
      for (const backendEdge of backendEdges) {
        const backend = nodeById.get(backendEdge.target);
        if (!backend || backend.data.namespace !== service.data.namespace) {
          continue;
        }
        const backendKind = kindOf(backend);
        if (!['pod', 'endpoint', 'endpointslice'].includes(backendKind)) {
          continue;
        }
        const laneBackend = laneNode(backend, laneId);
        addNode(laneBackend);
        addEdge(laneEdge(laneService, laneBackend, edgeKind(backendEdge)));

        if (backendKind !== 'pod') {
          continue;
        }
        for (const hostEdge of incomingEdges(backend.id, ['hosts'])) {
          const kubeNode = nodeById.get(hostEdge.source);
          if (!kubeNode || kindOf(kubeNode) !== 'node') {
            continue;
          }
          const laneKubeNode = laneNode(kubeNode, laneId);
          addNode(laneKubeNode);
          addEdge(laneEdge(laneBackend, laneKubeNode, 'runs_on'));
        }
      }
      return;
    }

    const externalEndpoints = externalEndpointsForService(service);
    if (externalEndpoints.length > 0) {
      for (const endpoint of externalEndpoints) {
        const laneEndpoint = laneNode(endpoint, laneId);
        addNode(laneEndpoint);
        addEdge(laneEdge(laneService, laneEndpoint, 'endpoint'));
      }
      return;
    }

    const lanePodSummary = laneNode(
      syntheticPodSummaryNode(String(service.data.namespace ?? ''), summarySource, podsForService(service)),
      laneId,
    );
    addNode(lanePodSummary);
    addEdge(laneEdge(laneService, lanePodSummary, 'selects'));
  };

  for (const ingress of ingressNodes) {
    const ingressNamespace = String(ingress.data.namespace ?? '');
    const controllerEdges = incomingEdges(ingress.id, ['controls']);

    const routeEdges = outgoingEdges(ingress.id, ['defines']);
    const routeRecords: HostRouteRecord[] = [];
    for (const routeEdge of routeEdges) {
      const route = nodeById.get(routeEdge.target);
      if (kindOf(route) !== 'route' || route?.data.namespace !== ingressNamespace) {
        continue;
      }

      const serviceEdge = outgoingEdges(route.id, ['routes'])[0];
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
        const nodePorts = pickControllerNodePorts(nodeById, incomingEdges(controller.id, ['forwards']));
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

          const podEdges = outgoingEdges(record.displayService.id, ['selects', 'endpointslice', 'endpoint', 'externalname']);
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

            const nodeHostEdges = incomingEdges(backend.id, ['hosts']);
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
      const serviceEdges = outgoingEdges(ingress.id, ['routes']);
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

  for (const gateway of gatewayNodes) {
    const gatewayNamespace = String(gateway.data.namespace ?? '');
    const controllerNodes = incomingEdges(gateway.id, ['controls'])
      .map((edge) => nodeById.get(edge.source))
      .filter((node): node is TopologyNode => Boolean(node && isControllerNode(node)));
    const externalNodes = incomingEdges(gateway.id, ['fronts'])
      .map((edge) => nodeById.get(edge.source))
      .filter((node): node is TopologyNode => Boolean(node));
    const routeRecords: ExternalRouteRecord[] = [];

    for (const routeEdge of outgoingEdges(gateway.id, ['attaches', 'rejected'])) {
      const route = nodeById.get(routeEdge.target);
      if (!route || kindOf(route) !== 'route') {
        continue;
      }
      const hosts = propertyValues(route.data.properties?.hostnames);
      const routeHosts = hosts.length > 0 ? hosts : ['*'];
      const paths = propertyValues(route.data.properties?.paths);
      const routePaths = paths.length > 0 ? paths : ['/'];
      const serviceNodes = outgoingEdges(route.id, ['routes'])
        .map((edge) => nodeById.get(edge.target))
        .filter((node): node is TopologyNode => Boolean(node && kindOf(node) === 'service'));

      for (const host of routeHosts) {
        const hostLaneId = ingressHostLaneId(gatewayNamespace, gateway, host);
        if (serviceNodes.length === 0) {
          const missing = syntheticMissingServiceNode(gatewayNamespace, route);
          routeRecords.push({ route, displayService: missing, host, path: routePaths[0], hostLaneId });
          for (const path of routePaths) {
            routeItems.push(routeItemFromExternal(route, gateway, hostLaneId, host, path));
          }
          continue;
        }
        for (const service of serviceNodes) {
          routeRecords.push({ route, service, displayService: service, host, path: routePaths[0], hostLaneId });
          for (const path of routePaths) {
            routeItems.push(routeItemFromExternal(route, gateway, hostLaneId, host, path, service));
          }
        }
      }
    }

    const recordsByHostLane = new Map<string, ExternalRouteRecord[]>();
    for (const record of routeRecords) {
      recordsByHostLane.set(record.hostLaneId, [...(recordsByHostLane.get(record.hostLaneId) ?? []), record]);
    }
    for (const [hostLaneId, records] of recordsByHostLane) {
      const laneGateway = laneNode(gateway, hostLaneId);
      addNode(laneGateway);

      const entries = externalNodes.length > 0 ? externalNodes : [namespaceTrafficNode(gatewayNamespace)];
      const controllers = controllerNodes.length > 0 ? controllerNodes : [];
      for (const entry of entries) {
        const laneEntry = laneNode(entry, hostLaneId);
        addNode(laneEntry);
        if (controllers.length === 0) {
          addEdge(laneEdge(laneEntry, laneGateway, 'traffic'));
          continue;
        }
        for (const controller of controllers) {
          const laneController = laneNode(controller, hostLaneId);
          addNode(laneController);
          addEdge(laneEdge(laneEntry, laneController, 'traffic'));
          addEdge(laneEdge(laneController, laneGateway, 'controls'));
        }
      }

      for (const record of records) {
        const laneRoute = laneNode(record.route, record.route.id);
        addNode(laneRoute);
        addEdge(laneEdge(laneGateway, laneRoute, record.route.data.status === 'Accepted=False' ? 'rejected' : 'attaches'));
        if (record.service) {
          addServiceBranch(laneRoute, record.service, record.route.id, record.route);
        } else {
          const laneMissing = laneNode(record.displayService, record.route.id);
          addNode(laneMissing);
          addEdge(laneEdge(laneRoute, laneMissing, 'missing'));
        }
      }
    }
  }

  for (const f5 of f5Nodes) {
    const f5Namespace = String(f5.data.namespace ?? '');
    const hosts = propertyValues(f5.data.properties?.hostnames);
    const routeHosts = hosts.length > 0 ? hosts : [String(f5.data.name || f5.data.label || 'F5')];
    const services = outgoingEdges(f5.id, ['balances'])
      .map((edge) => nodeById.get(edge.target))
      .filter((node): node is TopologyNode => Boolean(node && kindOf(node) === 'service'));

    for (const host of routeHosts) {
      const hostLaneId = ingressHostLaneId(f5Namespace, f5, host);
      const laneF5 = laneNode(f5, hostLaneId);
      addNode(laneF5);

      if (services.length === 0) {
        const missing = syntheticMissingServiceNode(f5Namespace, f5);
        const laneMissing = laneNode(missing, hostLaneId);
        addNode(laneMissing);
        addEdge(laneEdge(laneF5, laneMissing, 'missing'));
        routeItems.push(routeItemFromExternal(f5, f5, hostLaneId, host, '/'));
        continue;
      }

      for (const service of services) {
        const itemId = `${f5.id}:${service.id}`;
        routeItems.push({
          id: itemId,
          topologyId: hostLaneId,
          ingressId: f5.id,
          rootKind: String(f5.data.properties?.kind ?? 'F5'),
          serviceId: service.id,
          namespace: f5Namespace,
          name: String(f5.data.name ?? f5.data.label),
          host,
          path: '/',
          hostLaneId,
          ingress: ingressRouteLabel(f5),
          backend: nodeDisplayName(service),
          status: String(f5.data.status ?? 'Configured'),
          severity: routeSeverity(f5),
        });
        const summarySource = { ...f5, id: itemId };
        addServiceBranch(laneF5, service, hostLaneId, summarySource, 'balances');
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

function routeItemFromExternal(
  route: TopologyNode,
  root: TopologyNode,
  hostLaneId: string,
  host: string,
  path: string,
  service?: TopologyNode,
): RouteItem {
  const backend = service ? nodeDisplayName(service) : String(route.data.properties?.referenceError ?? 'missing service');
  return {
    id: `${route.id}:${safeVisualId(host)}:${safeVisualId(path)}:${service?.id ?? 'missing'}`,
    topologyId: route.id,
    ingressId: root.id,
    rootKind: String(root.data.kind ?? 'Gateway'),
    serviceId: service?.id ?? '',
    namespace: String(root.data.namespace ?? route.data.namespace ?? ''),
    name: String(route.data.label ?? route.data.name),
    host,
    path,
    hostLaneId,
    ingress: ingressRouteLabel(root),
    backend,
    status: String(route.data.status ?? 'Unknown'),
    severity: routeSeverity(route),
  };
}
