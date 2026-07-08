import { useEffect, useMemo, useState } from 'react';
import {
  Background,
  Controls,
  Panel,
  ReactFlow,
  useEdgesState,
  useNodesState,
  type ReactFlowInstance,
  type NodeTypes,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { KubeNode } from './components/KubeNode';
import {
  useTopologyStream,
  type TopologyEdge,
  type TopologyNode,
} from './hooks/useTopologyStream';
import {
  isControllerNode,
  isIngressNode,
  nodeDisplayName,
  summarizeKinds,
} from './topologyView';
import {
  filterEdgesForNodes,
  filterNodesByRoute,
  focusEdgesByRoute,
  focusNodesByRoute,
  groupRoutes,
  groupRoutesByHost,
  ingressHostLaneId,
  layoutTrafficPath,
  routeGroupButtonClass,
  routeGroupStatus,
  safeVisualId,
  severityRank,
  type RouteItem,
  type TopologyMode,
} from './trafficGraph';

const nodeTypes = {
  northscopeNode: KubeNode,
} satisfies NodeTypes;

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

interface NamespaceOption {
  name: string;
  ingressCount: number;
}

function statusLabel(status: string, hasSnapshot: boolean): string {
  if (status === 'connected') {
    return hasSnapshot ? 'Live config' : 'Syncing';
  }
  if (status === 'connecting') {
    return 'Connecting';
  }
  return 'Reconnecting';
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
    case 'endpointslice':
      return 'selects';
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
    type: 'step',
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

function routeMatchesSearch(route: RouteItem, query: string): boolean {
  const normalized = query.trim().toLowerCase();
  if (!normalized) {
    return true;
  }

  return [route.namespace, route.ingress, route.host, route.path, route.backend, route.status]
    .filter(Boolean)
    .some((value) => value.toLowerCase().includes(normalized));
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

function buildNamespaceTrafficGraph(
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
      .filter((edge) => edge.source === service.id && ['selects', 'endpointslice'].includes(edgeKind(edge)))
      .map((edge) => nodeById.get(edge.target))
      .filter((node): node is TopologyNode => {
        if (!node) {
          return false;
        }
        return kindOf(node) === 'pod' && node.data.namespace === service.data.namespace;
      });
  const externalEndpointsForService = (service: TopologyNode): TopologyNode[] =>
    edges
      .filter((edge) => edge.source === service.id && edgeKind(edge) === 'endpointslice')
      .map((edge) => nodeById.get(edge.target))
      .filter((node): node is TopologyNode => kindOf(node) === 'endpointslice' && node?.data.namespace === service.data.namespace);

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
            (edge) => edge.source === record.displayService.id && ['selects', 'endpointslice'].includes(edgeKind(edge)),
          );
          for (const podEdge of podEdges) {
            const backend = nodeById.get(podEdge.target);
            if (!backend || backend.data.namespace !== record.displayService.data.namespace) {
              continue;
            }
            if (kindOf(backend) === 'endpointslice') {
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
                addEdge(laneEdge(laneService, laneEndpoint, 'endpointslice'));
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

export default function App() {
  const { nodes, edges, snapshot, status, error } = useTopologyStream();
  const [namespace, setNamespace] = useState('');
  const [namespaceQuery, setNamespaceQuery] = useState('');
  const [namespacePickerOpen, setNamespacePickerOpen] = useState(false);
  const [routeSearch, setRouteSearch] = useState('');
  const [selectedHostGroupId, setSelectedHostGroupId] = useState('');
  const [topologyMode, setTopologyMode] = useState<TopologyMode>('simple');
  const [flowInstance, setFlowInstance] = useState<ReactFlowInstance<TopologyNode, TopologyEdge> | null>(null);
  const [flowNodes, setNodes, onNodesChange] = useNodesState<TopologyNode>([]);
  const [flowEdges, setEdges, onEdgesChange] = useEdgesState<TopologyEdge>([]);

  const namespaces = useMemo(
    () =>
      Array.from(new Set(nodes.map((node) => node.data.namespace).filter((value): value is string => Boolean(value)))).sort(),
    [nodes],
  );
  const namespaceOptions = useMemo<NamespaceOption[]>(() => {
    const ingressCounts = nodes.reduce((counts, node) => {
      if (isIngressNode(node) && node.data.namespace) {
        counts.set(node.data.namespace, (counts.get(node.data.namespace) ?? 0) + 1);
      }
      return counts;
    }, new Map<string, number>());

    return namespaces
      .map((name) => ({ name, ingressCount: ingressCounts.get(name) ?? 0 }))
      .sort((left, right) => {
        const ingressDelta = Number(right.ingressCount > 0) - Number(left.ingressCount > 0);
        if (ingressDelta !== 0) return ingressDelta;
        return left.name.localeCompare(right.name);
      });
  }, [namespaces, nodes]);
  const filteredNamespaceOptions = useMemo(() => {
    const query = namespaceQuery.trim().toLowerCase();
    if (!query) {
      return namespaceOptions;
    }
    return namespaceOptions.filter((option) => option.name.toLowerCase().includes(query));
  }, [namespaceOptions, namespaceQuery]);

  const namespaceGraph = useMemo(
    () => buildNamespaceTrafficGraph(namespace, nodes, edges, topologyMode),
    [edges, namespace, nodes, topologyMode],
  );

  const visibleGraphNodes = namespaceGraph.nodes;
  const visibleGraphEdges = namespaceGraph.edges;
  const graphReady = visibleGraphNodes.length > 0;
  const routes = namespaceGraph.routes;
  const filteredRoutes = useMemo(() => routes.filter((route) => routeMatchesSearch(route, routeSearch)), [routeSearch, routes]);
  const routeGroups = useMemo(() => groupRoutes(filteredRoutes), [filteredRoutes]);
  const routeHostGroups = useMemo(() => groupRoutesByHost(filteredRoutes), [filteredRoutes]);
  const routeIngressCount = useMemo(() => new Set(routes.map((route) => route.ingressId)).size, [routes]);
  const routeHostCount = useMemo(() => new Set(routes.map((route) => route.host)).size, [routes]);
  const selectedHostGroup = routeHostGroups.find((group) => group.id === selectedHostGroupId);
  const selectedRouteIds = useMemo(() => selectedHostGroup?.routes.map((route) => route.id) ?? [], [selectedHostGroup]);
  const selectedLaneIds = useMemo(
    () => Array.from(new Set(selectedHostGroup?.routes.map((route) => route.hostLaneId) ?? [])),
    [selectedHostGroup],
  );
  const displayGraphNodes = useMemo(() => {
    if ((graphReady && filteredRoutes.length === 0) || !selectedHostGroup) {
      return [];
    }
    const selectedNodes = filterNodesByRoute(visibleGraphNodes, selectedRouteIds, selectedLaneIds);
    return layoutTrafficPath(selectedNodes, topologyMode);
  }, [filteredRoutes.length, graphReady, selectedHostGroup, selectedLaneIds, selectedRouteIds, topologyMode, visibleGraphNodes]);
  const displayGraphEdges = useMemo(() => {
    return filterEdgesForNodes(visibleGraphEdges, displayGraphNodes);
  }, [displayGraphNodes, topologyMode, visibleGraphEdges]);
  const focusedGraphNodes = useMemo(
    () => focusNodesByRoute(displayGraphNodes, selectedRouteIds, selectedLaneIds),
    [displayGraphNodes, selectedLaneIds, selectedRouteIds],
  );
  const focusedGraphEdges = useMemo(
    () => focusEdgesByRoute(displayGraphEdges, selectedRouteIds, selectedLaneIds),
    [displayGraphEdges, selectedLaneIds, selectedRouteIds],
  );
  const hasSnapshot = Boolean(snapshot);
  const hasTopology = Boolean(snapshot && snapshot.nodes.length > 0);
  const summary = useMemo(() => summarizeKinds(nodes), [nodes]);
  const clusterInventory = useMemo(
    () =>
      `${summary.controllers} controller${summary.controllers === 1 ? '' : 's'} · ${summary.ingresses} ingress object${
        summary.ingresses === 1 ? '' : 's'
      } · ${summary.services} service${summary.services === 1 ? '' : 's'} · ${summary.pods} pod${summary.pods === 1 ? '' : 's'} · ${
        summary.nodes
      } node${summary.nodes === 1 ? '' : 's'}`,
    [summary],
  );

  const emptyStateTitle = useMemo(() => {
    if (!hasSnapshot) {
      return status === 'connected' ? 'Connected; waiting for first Kubernetes config snapshot' : 'Waiting for topology stream';
    }
    if (graphReady && filteredRoutes.length > 0 && !selectedHostGroup) {
      return 'Select a host route to draw topology';
    }
    if (hasTopology) {
      return namespace ? 'No Ingress routes found in this namespace' : 'No Ingress routes found';
    }
    return 'Snapshot received, but no supported topology objects were found';
  }, [filteredRoutes.length, graphReady, hasSnapshot, hasTopology, namespace, selectedHostGroup, status]);

  const emptyStateDescription = useMemo(() => {
    if (!hasSnapshot) {
      return 'If this stays here, check pod logs and read-only RBAC for ingresses, services, pods, endpointslices, and ingressclasses.';
    }
    if (graphReady && filteredRoutes.length > 0 && !selectedHostGroup) {
      return 'Search by host, ingress, service, path, or namespace, then select a host route from the list.';
    }
    if (hasTopology) {
      return namespace
        ? 'NorthScope needs Ingress objects with HTTP rules or default backends in the selected namespace.'
        : 'Search by host, ingress, service, path, or namespace to find a traffic route.';
    }
    return 'The cluster may be empty for watched resources, or NorthScope may not have permission to list them.';
  }, [filteredRoutes.length, graphReady, hasSnapshot, hasTopology, namespace, selectedHostGroup]);

  const selectNamespace = (value: string) => {
    setNamespace(value);
    setNamespaceQuery(value);
    setNamespacePickerOpen(false);
    setRouteSearch('');
    setSelectedHostGroupId('');
  };

  const clearNamespace = () => {
    setNamespace('');
    setNamespaceQuery('');
    setNamespacePickerOpen(false);
    setRouteSearch('');
    setSelectedHostGroupId('');
  };

  const namespacePlaceholder = hasSnapshot ? 'All namespaces' : 'Waiting for namespaces';

  useEffect(() => {
    setNodes(focusedGraphNodes);
    setEdges(focusedGraphEdges);
  }, [focusedGraphEdges, focusedGraphNodes, setEdges, setNodes]);

  useEffect(() => {
    if (!flowInstance || flowNodes.length === 0) {
      return;
    }

    const frame = window.requestAnimationFrame(() => {
      void flowInstance.fitView({ padding: topologyMode === 'simple' ? 0.28 : 0.18, duration: 180 });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [flowEdges, flowInstance, flowNodes, selectedHostGroup?.id, topologyMode]);

  useEffect(() => {
    if (selectedHostGroupId && !routeHostGroups.some((group) => group.id === selectedHostGroupId)) {
      setSelectedHostGroupId('');
    }
  }, [routeHostGroups, selectedHostGroupId]);

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-slate-100 text-slate-950">
      <header className="shrink-0 border-b border-slate-200 bg-white px-4 py-3 shadow-sm">
        <div className="flex min-h-10 flex-wrap items-center gap-3">
          <div className="mr-2 min-w-[210px]">
            <div className="text-sm font-black tracking-tight">NorthScope</div>
            <div className="text-[11px] font-medium text-slate-500">Kubernetes ingress traffic path debugger</div>
          </div>
          <div className="hidden min-w-0 flex-1 justify-end xl:flex">
            <div className="max-w-full truncate rounded-md border border-slate-200 bg-slate-50 px-3 py-1.5 text-[11px] font-semibold text-slate-500">
              <span className="font-black uppercase tracking-wide text-slate-400">Cluster inventory</span>
              <span className="ml-2 normal-case tracking-normal text-slate-600">{clusterInventory}</span>
            </div>
          </div>
          <div className="ml-auto flex h-9 overflow-hidden rounded-md border border-slate-300 bg-white p-0.5 text-xs font-black shadow-sm">
            <button
              type="button"
              onClick={() => setTopologyMode('simple')}
              className={`px-3 transition ${topologyMode === 'simple' ? 'rounded bg-slate-950 text-white shadow-sm' : 'text-slate-500 hover:text-slate-900'}`}
            >
              Simple
            </button>
            <button
              type="button"
              onClick={() => setTopologyMode('expanded')}
              className={`px-3 transition ${topologyMode === 'expanded' ? 'rounded bg-slate-950 text-white shadow-sm' : 'text-slate-500 hover:text-slate-900'}`}
            >
              Expanded
            </button>
          </div>
          <div
            className={`rounded-full px-3 py-1 text-xs font-bold ${
              status === 'connected' && hasSnapshot
                ? 'bg-emerald-100 text-emerald-700'
                : status === 'connecting' || (status === 'connected' && !hasSnapshot)
                  ? 'bg-amber-100 text-amber-700'
                  : 'bg-red-100 text-red-700'
            }`}
          >
            {statusLabel(status, hasSnapshot)}
          </div>
        </div>
      </header>

      <main className="flex min-h-0 flex-1">
        <aside className="flex w-[312px] shrink-0 flex-col border-r border-slate-200 bg-white">
          <section className="shrink-0 border-b border-slate-100 px-4 py-3">
            <label className="text-[11px] font-black uppercase tracking-wide text-slate-500" htmlFor="namespace-search">
              Namespace
            </label>
            <div className="relative mt-1.5">
              <input
                id="namespace-search"
                value={namespacePickerOpen ? namespaceQuery : namespace || 'All namespaces'}
                placeholder={namespacePlaceholder}
                onFocus={() => {
                  setNamespaceQuery(namespace);
                  setNamespacePickerOpen(true);
                }}
                onBlur={() => {
                  window.setTimeout(() => {
                    setNamespaceQuery(namespace);
                    setNamespacePickerOpen(false);
                  }, 120);
                }}
                onChange={(event) => {
                  const value = event.target.value;
                  setNamespaceQuery(value);
                  if (!value.trim() && namespace) {
                    setNamespace('');
                    setRouteSearch('');
                    setSelectedHostGroupId('');
                  }
                  setNamespacePickerOpen(true);
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    const normalizedNamespaceQuery = namespaceQuery.trim().toLowerCase();
                    if (!normalizedNamespaceQuery || normalizedNamespaceQuery === 'all' || normalizedNamespaceQuery === 'all namespaces') {
                      clearNamespace();
                      return;
                    }
                    const selected = filteredNamespaceOptions[0]?.name;
                    if (selected) {
                      selectNamespace(selected);
                    }
                  }
                  if (event.key === 'Escape') {
                    if (!namespaceQuery.trim()) {
                      clearNamespace();
                    } else if (namespacePickerOpen && namespaceQuery.trim() !== namespace) {
                      setNamespaceQuery(namespace);
                    } else {
                      clearNamespace();
                    }
                    setNamespacePickerOpen(false);
                  }
                }}
                className="h-9 w-full rounded-md border border-slate-300 bg-white px-3 pr-16 text-sm font-semibold outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
              />
              {namespace || namespaceQuery ? (
                <button
                  type="button"
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={clearNamespace}
                  className="absolute right-1.5 top-1.5 h-6 rounded px-2 text-[11px] font-black uppercase text-slate-500 transition hover:bg-slate-100 hover:text-slate-900"
                  aria-label="Clear namespace"
                >
                  Clear
                </button>
              ) : null}
              {namespacePickerOpen ? (
                <div className="absolute left-0 right-0 top-10 z-30 max-h-72 overflow-auto rounded-md border border-slate-200 bg-white py-1 shadow-lg">
                  {!namespaceQuery.trim() || 'all namespaces'.includes(namespaceQuery.trim().toLowerCase()) ? (
                    <button
                      type="button"
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={clearNamespace}
                      className={`flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm font-semibold transition hover:bg-blue-50 ${
                        !namespace ? 'bg-blue-50 text-blue-800' : 'text-slate-800'
                      }`}
                    >
                      <span className="min-w-0 truncate">All namespaces</span>
                      <span className="shrink-0 text-[10px] font-black uppercase text-slate-400">all</span>
                    </button>
                  ) : null}
                  {filteredNamespaceOptions.length > 0 ? (
                    filteredNamespaceOptions.map((option) => (
                      <button
                        key={option.name}
                        type="button"
                        onMouseDown={(event) => event.preventDefault()}
                        onClick={() => selectNamespace(option.name)}
                        className={`flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm font-semibold transition hover:bg-blue-50 ${
                          option.name === namespace ? 'bg-blue-50 text-blue-800' : 'text-slate-800'
                        }`}
                      >
                        <span className="min-w-0 truncate">{option.name}</span>
                        <span className={`shrink-0 text-[10px] font-black uppercase ${option.ingressCount > 0 ? 'text-blue-600' : 'text-slate-400'}`}>
                          {option.ingressCount} ing
                        </span>
                      </button>
                    ))
                  ) : (
                    <div className="px-3 py-2 text-sm font-semibold text-slate-500">No namespace matches</div>
                  )}
                </div>
              ) : null}
            </div>
          </section>

          <section className="flex min-h-0 flex-1 flex-col">
            <div className="shrink-0 px-4 py-3">
              <div className="text-[11px] font-black uppercase tracking-wide text-slate-500">Ingress routes</div>
              <div className="mt-1 text-sm font-semibold text-slate-900">
                {routeIngressCount} Ingress Object{routeIngressCount === 1 ? '' : 's'}
              </div>
              <div className="mt-0.5 text-[11px] font-semibold text-slate-500">
                {routeHostCount} host{routeHostCount === 1 ? '' : 's'} / {routes.length} path{routes.length === 1 ? '' : 's'}
              </div>
              <div className="relative mt-3">
                <input
                  value={routeSearch}
                  onChange={(event) => setRouteSearch(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Escape') {
                      setRouteSearch('');
                    }
                  }}
                  placeholder="Search ingress, host, path, service"
                  className="h-8 w-full rounded-md border border-slate-200 bg-slate-50 px-3 pr-16 text-xs font-semibold outline-none transition placeholder:text-slate-400 focus:border-blue-500 focus:bg-white focus:ring-2 focus:ring-blue-100"
                />
                {routeSearch ? (
                  <button
                    type="button"
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => setRouteSearch('')}
                    className="absolute right-1 top-1 h-6 rounded px-2 text-[10px] font-black uppercase text-slate-500 transition hover:bg-slate-100 hover:text-slate-900"
                    aria-label="Clear route search"
                  >
                    Clear
                  </button>
                ) : null}
              </div>
            </div>
            <div className="min-h-0 flex-1 space-y-3 overflow-auto px-3 pb-3">
              {graphReady && routeGroups.length > 0 ? (
                routeGroups.map((group) => {
                  const routesByHost = Array.from(
                    group.routes.reduce((items, route) => {
                      items.set(route.hostLaneId, [...(items.get(route.hostLaneId) ?? []), route]);
                      return items;
                    }, new Map<string, RouteItem[]>()),
                  );
                  const groupSelected = group.routes.some((route) => route.hostLaneId === selectedHostGroup?.id);
                  return (
                    <div
                      key={group.id}
                      className={routeGroupButtonClass(group, groupSelected)}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <div className="truncate text-sm font-black">{namespace ? group.ingress : `${group.namespace} / ${group.ingress}`}</div>
                          <div className="mt-0.5 text-[11px] font-semibold opacity-75">
                            {routesByHost.length} host{routesByHost.length === 1 ? '' : 's'} / {group.routes.length} path
                            {group.routes.length === 1 ? '' : 's'}
                          </div>
                        </div>
                        <span className="shrink-0 rounded-full bg-white/70 px-2 py-0.5 text-[10px] font-black uppercase text-slate-700">
                          {routeGroupStatus(group)}
                        </span>
                      </div>
                      <div className="mt-2 space-y-2">
                        {routesByHost.map(([hostLaneId, hostRoutes]) => {
                          const hostSelected = hostLaneId === selectedHostGroup?.id;
                          const host = hostRoutes[0]?.host ?? 'unknown host';
                          return (
                            <button
                              key={hostLaneId}
                              type="button"
                              onClick={() => setSelectedHostGroupId(hostLaneId)}
                              className={`w-full cursor-pointer space-y-1 rounded-md border-2 p-1.5 text-left shadow-sm transition ${
                                hostSelected
                                  ? 'border-slate-950 bg-white text-slate-950 ring-2 ring-blue-100'
                                  : 'border-slate-300 bg-white text-slate-900 hover:border-blue-400 hover:bg-white hover:ring-2 hover:ring-blue-100'
                              }`}
                            >
                              <div className="flex min-w-0 items-center justify-between gap-2 px-0.5">
                                <div className="truncate text-xs font-black">{host}</div>
                                <div className="flex shrink-0 items-center gap-1">
                                  {hostRoutes.length > 1 ? (
                                    <span className="text-[10px] font-black uppercase text-slate-500">{hostRoutes.length} paths</span>
                                  ) : null}
                                  <span
                                    className={`rounded-full px-1.5 py-0.5 text-[9px] font-black uppercase ${
                                      hostSelected ? 'bg-slate-950 text-white' : 'border border-blue-200 bg-blue-50 text-blue-700'
                                    }`}
                                  >
                                    {hostSelected ? 'Selected' : 'View'}
                                  </span>
                                </div>
                              </div>
                              {hostRoutes.map((route) => (
                                <div key={route.id} className={`rounded border px-2 py-1 ${hostSelected ? 'border-slate-200 bg-slate-50' : 'border-slate-100 bg-slate-50/80'}`}>
                                  <div className="flex min-w-0 items-center gap-2">
                                    <span className="min-w-0 truncate text-xs font-black">{route.path}</span>
                                    <span className="shrink-0 text-[10px] font-bold uppercase opacity-70">{route.status}</span>
                                  </div>
                                  <div className="mt-0.5 truncate text-[11px] font-semibold opacity-75">{route.backend}</div>
                                </div>
                              ))}
                            </button>
                          );
                        })}
                      </div>
                    </div>
                  );
                })
              ) : (
                <div className="rounded-md border border-dashed border-slate-200 bg-slate-50 px-3 py-4 text-sm font-semibold text-slate-500">
                  {routeSearch
                    ? 'No routes match this search'
                    : namespace
                      ? 'No Ingress routes in this namespace'
                      : 'No Ingress routes found'}
                </div>
              )}
            </div>
          </section>
        </aside>

        <section className="relative min-w-0 flex-1">
          <ReactFlow<TopologyNode, TopologyEdge>
            key={`${namespace || 'none'}:${topologyMode}:${selectedHostGroup?.id ?? 'none'}`}
            nodes={flowNodes}
            edges={flowEdges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onInit={setFlowInstance}
            fitView
            fitViewOptions={{ padding: 0.18 }}
          >
            <Background color="#d9dee8" gap={24} />
            <Controls position="bottom-right" />
            {error ? (
              <Panel position="top-right" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs font-semibold text-red-700 shadow">
                {error}
              </Panel>
            ) : null}
          </ReactFlow>

          {displayGraphNodes.length === 0 ? (
            <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
              <div className="rounded-md border border-dashed border-slate-300 bg-white/95 px-7 py-5 text-center shadow-sm">
                <div className="text-base font-bold text-slate-900">{emptyStateTitle}</div>
                <div className="mt-1 max-w-[560px] text-sm text-slate-500">{emptyStateDescription}</div>
              </div>
            </div>
          ) : null}
        </section>
      </main>
    </div>
  );
}
