import { useEffect, useMemo, useState } from 'react';
import {
  Background,
  Controls,
  Panel,
  ReactFlow,
  useEdgesState,
  useNodesState,
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

const nodeTypes = {
  northscopeNode: KubeNode,
} satisfies NodeTypes;

const TRAFFIC_NODE_ID_PREFIX = 'visual:f5-edge';

interface RouteItem {
  id: string;
  ingressId: string;
  serviceId: string;
  name: string;
  host: string;
  path: string;
  hostLaneId: string;
  ingress: string;
  backend: string;
  status: string;
  severity: string;
}

interface NamespaceTrafficGraph {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
  routes: RouteItem[];
}

type TopologyMode = 'simple' | 'expanded';

interface RouteGroup {
  id: string;
  host: string;
  ingress: string;
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

function displayEdge(edge: TopologyEdge): TopologyEdge {
  const kind = edgeKind(edge);
  return {
    ...edge,
    label: edgeLabel(kind),
  };
}

function syntheticEdge(source: string, target: string, kind: string, label: string): TopologyEdge {
  return {
    id: `${source}->${target}:${kind}`,
    source,
    target,
    type: 'smoothstep',
    label: label || edgeLabel(kind),
    animated: kind === 'traffic',
    data: { kind },
  };
}

function namespaceTrafficNode(namespace: string): TopologyNode {
  return {
    id: `${TRAFFIC_NODE_ID_PREFIX}:${namespace}`,
    type: 'northscopeNode',
    position: { x: 0, y: 0 },
    data: {
      label: 'External edge',
      kind: 'ExternalEdge',
      namespace,
      name: 'External edge',
      status: 'Assumed entry',
      properties: {
        role: 'F5 / LB assumed entry',
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

function nodeColumn(node: TopologyNode): string {
  const kind = kindOf(node);
  if (kind === 'f5' || kind === 'externaledge') return 'f5';
  if (kind === 'podgroup') return 'pod';
  return kind;
}

function layoutTrafficPath(nodes: TopologyNode[], mode: TopologyMode): TopologyNode[] {
  const columnOrder =
    mode === 'simple'
      ? ['f5', 'nodeport', 'controller', 'ingress', 'service', 'pod']
      : ['f5', 'nodeport', 'controller', 'ingress', 'dns', 'route', 'service', 'pod', 'node'];
  const columnX: Record<string, number> =
    mode === 'simple'
      ? {
          f5: 0,
          nodeport: 240,
          controller: 500,
          ingress: 760,
          service: 1020,
          pod: 1280,
        }
      : {
          f5: 0,
          nodeport: 280,
          controller: 580,
          ingress: 880,
          dns: 1180,
          route: 1480,
          service: 1800,
          pod: 2120,
          node: 2440,
        };
  const rowGap = mode === 'simple' ? 190 : 230;
  const columns = new Map<string, TopologyNode[]>();

  for (const node of nodes) {
    const kind = nodeColumn(node);
    const column = columnOrder.includes(kind) ? kind : 'route';
    columns.set(column, [...(columns.get(column) ?? []), node]);
  }

  return nodes.map((node) => {
    const kind = nodeColumn(node);
    const column = columnOrder.includes(kind) ? kind : 'route';
    const columnNodes = [...(columns.get(column) ?? [])].sort((left, right) => {
      const leftLane = String(left.data.properties?.visualLane ?? '');
      const rightLane = String(right.data.properties?.visualLane ?? '');
      const laneOrder = leftLane.localeCompare(rightLane);
      if (laneOrder !== 0) return laneOrder;
      return nodeDisplayName(left).localeCompare(nodeDisplayName(right));
    });
    const rowIndex = Math.max(0, columnNodes.findIndex((item) => item.id === node.id));

    return {
      ...node,
      position: {
        x: columnX[column],
        y: rowIndex * rowGap,
      },
    };
  });
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

function routeItemFromNode(route: TopologyNode, ingress: TopologyNode, hostLaneId: string, service?: TopologyNode): RouteItem {
  const props = route.data.properties ?? {};
  const host = routeHost(route);
  const path = routePath(route);
  return {
    id: route.id,
    ingressId: ingress.id,
    serviceId: service?.id ?? '',
    name: String(route.data.label ?? route.data.name),
    host,
    path,
    hostLaneId,
    ingress: nodeDisplayName(ingress),
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

function severityClass(severity: string): string {
  const normalized = severity.toLowerCase();
  if (normalized === 'error') {
    return 'border-red-200 bg-red-50 text-red-800';
  }
  if (normalized === 'warning') {
    return 'border-amber-200 bg-amber-50 text-amber-800';
  }
  if (normalized === 'ok') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-800';
  }
  return 'border-slate-200 bg-slate-50 text-slate-700';
}

function routeButtonClass(route: RouteItem, selected: boolean): string {
  return [
    'w-full rounded-md border px-3 py-2 text-left transition',
    selected ? 'border-slate-900 bg-slate-900 text-white shadow-sm' : severityClass(route.severity),
  ].join(' ');
}

function severityRank(severity: string): number {
  const normalized = severity.toLowerCase();
  if (normalized === 'error') return 0;
  if (normalized === 'warning') return 1;
  if (normalized === 'ok') return 2;
  return 3;
}

function groupRoutes(routes: RouteItem[]): RouteGroup[] {
  const groups = new Map<string, RouteItem[]>();
  for (const route of routes) {
    groups.set(route.hostLaneId, [...(groups.get(route.hostLaneId) ?? []), route]);
  }
  return Array.from(groups.entries())
    .sort(([, leftGroup], [, rightGroup]) => {
      const host = leftGroup[0].host.localeCompare(rightGroup[0].host);
      if (host !== 0) return host;
      return leftGroup[0].ingress.localeCompare(rightGroup[0].ingress);
    })
    .map(([id, group]) => ({
      id,
      host: group[0].host,
      ingress: group[0].ingress,
      routes: [...group].sort((left, right) => {
        const severity = severityRank(left.severity) - severityRank(right.severity);
        if (severity !== 0) return severity;
        return left.path.localeCompare(right.path);
      }),
    }));
}

function safeVisualId(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9-]+/g, '-');
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

function hostLaneId(namespace: string, host: string): string {
  return `host:${namespace}:${safeVisualId(host)}`;
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

function laneIdForNode(node: TopologyNode): string {
  return String(node.data.properties?.visualLane ?? '');
}

function laneIdFromNodeId(id: string): string {
  const marker = ':lane:';
  const laneIndex = id.lastIndexOf(marker);
  if (laneIndex === -1) {
    return '';
  }
  return id.slice(laneIndex + marker.length);
}

function laneIdsForEdge(edge: TopologyEdge): string[] {
  return [laneIdFromNodeId(edge.source), laneIdFromNodeId(edge.target)].filter(Boolean);
}

function focusNodesByRoute(nodes: TopologyNode[], selectedRouteId: string, selectedHostLaneId: string): TopologyNode[] {
  if (!selectedRouteId && !selectedHostLaneId) {
    return nodes;
  }

  return nodes.map((node) => {
    const laneId = laneIdForNode(node);
    const active = laneId === selectedRouteId || laneId === selectedHostLaneId;
    return {
      ...node,
      style: {
        ...(node.style ?? {}),
        opacity: active ? 1 : 0.22,
      },
      zIndex: active ? 20 : 0,
    };
  });
}

function focusEdgesByRoute(edges: TopologyEdge[], selectedRouteId: string, selectedHostLaneId: string): TopologyEdge[] {
  if (!selectedRouteId && !selectedHostLaneId) {
    return edges;
  }

  return edges.map((edge) => {
    const laneIds = laneIdsForEdge(edge);
    const active = laneIds.includes(selectedRouteId) || laneIds.includes(selectedHostLaneId);
    return {
      ...edge,
      animated: Boolean(edge.animated && active),
      style: {
        ...(edge.style ?? {}),
        strokeOpacity: active ? 0.95 : 0.16,
        strokeWidth: active ? 2.6 : 1.2,
      },
      zIndex: active ? 20 : 0,
    };
  });
}

function buildNamespaceTrafficGraph(
  namespace: string,
  nodes: TopologyNode[],
  edges: TopologyEdge[],
  mode: TopologyMode,
): NamespaceTrafficGraph {
  if (!namespace) {
    return { nodes: [], edges: [], routes: [] };
  }

  const nodeById = new Map(nodes.map((node) => [node.id, node]));
  const ingressNodes = nodes.filter((node) => isIngressNode(node) && node.data.namespace === namespace);
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
        return kindOf(node) === 'pod' && node.data.namespace === namespace;
      });

  for (const ingress of ingressNodes) {
    const controllerEdges = edges.filter((edge) => edge.target === ingress.id && edgeKind(edge) === 'controls');

    const routeEdges = edges.filter((edge) => edge.source === ingress.id && edgeKind(edge) === 'defines');
    const routeRecords: HostRouteRecord[] = [];
    for (const routeEdge of routeEdges) {
      const route = nodeById.get(routeEdge.target);
      if (kindOf(route) !== 'route' || route?.data.namespace !== namespace) {
        continue;
      }

      const serviceEdge = edges.find((edge) => edge.source === route.id && edgeKind(edge) === 'routes');
      const service = serviceEdge ? nodeById.get(serviceEdge.target) : undefined;
      const displayService = service ?? syntheticMissingServiceNode(namespace, route);
      const host = routeHost(route);
      const routeHostLaneId = hostLaneId(namespace, host);
      routeItems.push(routeItemFromNode(route, ingress, routeHostLaneId, displayService));
      routeRecords.push({ route, serviceEdge, service, displayService, host, hostLaneId: routeHostLaneId });
    }

    const recordsByHostLane = new Map<string, HostRouteRecord[]>();
    for (const record of routeRecords) {
      recordsByHostLane.set(record.hostLaneId, [...(recordsByHostLane.get(record.hostLaneId) ?? []), record]);
    }

    for (const [routeHostLaneId, hostRecords] of recordsByHostLane.entries()) {
      const host = hostRecords[0].host;
      const laneExternal = laneNode(namespaceTrafficNode(namespace), routeHostLaneId);
      const laneIngress = laneNode(ingress, routeHostLaneId);

      addNode(laneExternal);
      addNode(laneIngress);

      const controllers = controllerEdges
        .map((edge) => nodeById.get(edge.source))
        .filter((node): node is TopologyNode => (node ? isControllerNode(node) : false));
      const controllersForLane = controllers.length > 0 ? controllers : [syntheticControllerNode(namespace)];

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
        const laneHost = laneNode(syntheticHostNode(namespace, host), routeHostLaneId);
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
            const pod = nodeById.get(podEdge.target);
            if (kindOf(pod) !== 'pod' || pod?.data.namespace !== namespace) {
              continue;
            }
            const lanePod = laneNode(pod, record.route.id);
            addNode(lanePod);
            addEdge(laneEdge(laneService, lanePod, edgeKind(podEdge)));

            const nodeHostEdges = edges.filter((edge) => edge.target === pod.id && edgeKind(edge) === 'hosts');
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
            const lanePodSummary = laneNode(syntheticPodSummaryNode(namespace, record.route, podsForService(record.displayService)), record.route.id);
            addNode(lanePodSummary);
            addEdge(laneEdge(laneService, lanePodSummary, 'selects'));
          }
        }
      }
    }

    if (routeEdges.length === 0) {
      const serviceEdges = edges.filter((edge) => edge.source === ingress.id && edgeKind(edge) === 'routes');
      for (const serviceEdge of serviceEdges) {
        const service = nodeById.get(serviceEdge.target);
        if (kindOf(service) !== 'service' || service?.data.namespace !== namespace) {
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
  const [selectedRouteId, setSelectedRouteId] = useState('');
  const [topologyMode, setTopologyMode] = useState<TopologyMode>('simple');
  const [flowNodes, setNodes, onNodesChange] = useNodesState<TopologyNode>([]);
  const [flowEdges, setEdges, onEdgesChange] = useEdgesState<TopologyEdge>([]);

  const namespaces = useMemo(
    () =>
      Array.from(new Set(nodes.map((node) => node.data.namespace).filter((value): value is string => Boolean(value)))).sort(),
    [nodes],
  );

  const namespaceGraph = useMemo(
    () => buildNamespaceTrafficGraph(namespace, nodes, edges, topologyMode),
    [edges, namespace, nodes, topologyMode],
  );

  const visibleGraphNodes = namespaceGraph.nodes;
  const visibleGraphEdges = namespaceGraph.edges;
  const routes = namespaceGraph.routes;
  const routeGroups = useMemo(() => groupRoutes(routes), [routes]);
  const selectedRoute = routes.find((route) => route.id === selectedRouteId) ?? routes[0];
  const focusedGraphNodes = useMemo(
    () => focusNodesByRoute(visibleGraphNodes, selectedRoute?.id ?? '', selectedRoute?.hostLaneId ?? ''),
    [selectedRoute?.hostLaneId, selectedRoute?.id, visibleGraphNodes],
  );
  const focusedGraphEdges = useMemo(
    () => focusEdgesByRoute(visibleGraphEdges, selectedRoute?.id ?? '', selectedRoute?.hostLaneId ?? ''),
    [selectedRoute?.hostLaneId, selectedRoute?.id, visibleGraphEdges],
  );
  const hasSnapshot = Boolean(snapshot);
  const hasTopology = Boolean(snapshot && snapshot.nodes.length > 0);
  const graphReady = visibleGraphNodes.length > 0;
  const summary = useMemo(() => summarizeKinds(nodes), [nodes]);

  const headerStatusText = useMemo(() => {
    if (!snapshot) {
      if (status === 'connected') {
        return 'Connected; syncing Kubernetes config cache';
      }
      if (status === 'connecting') {
        return 'Opening topology stream';
      }
      return 'Waiting for topology';
    }
    if (!namespace) {
      return 'Select a namespace to render ingress traffic paths';
    }
    if (graphReady) {
      return `${routes.length} ingress traffic paths in ${namespace}`;
    }
    if (hasTopology) {
      return `No ingress traffic paths found in ${namespace}`;
    }
    return 'Snapshot received: 0 supported objects';
  }, [graphReady, hasTopology, namespace, routes.length, snapshot, status]);

  const emptyStateTitle = useMemo(() => {
    if (!hasSnapshot) {
      return status === 'connected' ? 'Connected; waiting for first Kubernetes config snapshot' : 'Waiting for topology stream';
    }
    if (!namespace) {
      return 'Select a namespace';
    }
    if (hasTopology) {
      return 'No Ingress routes found in this namespace';
    }
    return 'Snapshot received, but no supported topology objects were found';
  }, [hasSnapshot, hasTopology, namespace, status]);

  const emptyStateDescription = useMemo(() => {
    if (!hasSnapshot) {
      return 'If this stays here, check pod logs and read-only RBAC for ingresses, services, pods, endpointslices, and ingressclasses.';
    }
    if (!namespace) {
      return 'NorthScope draws External edge -> controller -> ingress -> route -> service -> pod -> node paths for the selected namespace.';
    }
    if (hasTopology) {
      return 'NorthScope needs Ingress objects with HTTP rules or default backends in the selected namespace.';
    }
    return 'The cluster may be empty for watched resources, or NorthScope may not have permission to list them.';
  }, [hasSnapshot, hasTopology, namespace]);

  useEffect(() => {
    setNodes(focusedGraphNodes);
    setEdges(focusedGraphEdges);
  }, [focusedGraphEdges, focusedGraphNodes, setEdges, setNodes]);

  useEffect(() => {
    if (!routes.some((route) => route.id === selectedRouteId)) {
      setSelectedRouteId(routes[0]?.id ?? '');
    }
  }, [routes, selectedRouteId]);

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-slate-100 text-slate-950">
      <header className="shrink-0 border-b border-slate-200 bg-white px-4 py-3 shadow-sm">
        <div className="flex min-h-10 flex-wrap items-center gap-3">
          <div className="mr-2 min-w-[210px]">
            <div className="text-sm font-black tracking-tight">NorthScope</div>
            <div className="text-[11px] font-medium text-slate-500">{headerStatusText}</div>
          </div>
          <select
            value={namespace}
            onChange={(event) => setNamespace(event.target.value)}
            className="h-9 w-[280px] rounded-md border border-slate-300 bg-white px-3 text-sm font-semibold outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
          >
            <option value="">Namespace</option>
            {namespaces.map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
          <div className="flex flex-wrap items-center gap-2 text-[10px] font-bold uppercase tracking-wide text-slate-500">
            <span className="rounded-full border border-slate-200 bg-white px-2 py-0.5 text-slate-500">Cluster totals</span>
            <span className="rounded-full bg-amber-50 px-2 py-0.5 text-amber-700">Controllers {summary.controllers}</span>
            <span className="rounded-full bg-blue-50 px-2 py-0.5 text-blue-700">Ingress {summary.ingresses}</span>
            <span className="rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700">Services {summary.services}</span>
            <span className="rounded-full bg-violet-50 px-2 py-0.5 text-violet-700">Pods {summary.pods}</span>
            <span className="rounded-full bg-zinc-100 px-2 py-0.5 text-zinc-700">Nodes {summary.nodes}</span>
          </div>
          <div className="ml-auto flex h-9 overflow-hidden rounded-md border border-slate-200 bg-slate-50 p-0.5 text-xs font-black">
            <button
              type="button"
              onClick={() => setTopologyMode('simple')}
              className={`px-3 transition ${topologyMode === 'simple' ? 'rounded bg-white text-slate-950 shadow-sm' : 'text-slate-500'}`}
            >
              Simple
            </button>
            <button
              type="button"
              onClick={() => setTopologyMode('expanded')}
              className={`px-3 transition ${topologyMode === 'expanded' ? 'rounded bg-white text-slate-950 shadow-sm' : 'text-slate-500'}`}
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
        {graphReady ? (
          <aside className="flex w-[288px] shrink-0 flex-col border-r border-slate-200 bg-white">
            <section className="flex min-h-0 flex-1 flex-col">
              <div className="shrink-0 px-4 py-3">
                <div className="text-[11px] font-black uppercase tracking-wide text-slate-500">Ingress routes</div>
                <div className="mt-1 text-sm font-semibold text-slate-900">
                  {routeGroups.length} host{routeGroups.length === 1 ? '' : 's'} / {routes.length} path{routes.length === 1 ? '' : 's'}
                </div>
              </div>
              <div className="min-h-0 flex-1 space-y-3 overflow-auto px-3 pb-3">
                {routeGroups.map((group) => (
                  <div key={group.id}>
                    <div className="mb-1.5 px-1">
                      <div className="truncate text-[12px] font-black text-slate-800">{group.host}</div>
                    </div>
                    <div className="space-y-2">
                      {group.routes.map((route) => (
                        <button
                          key={route.id}
                          type="button"
                          onClick={() => setSelectedRouteId(route.id)}
                          className={routeButtonClass(route, route.id === selectedRoute?.id)}
                        >
                          <div className="flex items-center justify-between gap-2">
                            <span className="min-w-0 truncate text-sm font-black">{route.path}</span>
                            <span className="shrink-0 rounded-full bg-white/70 px-2 py-0.5 text-[10px] font-black uppercase text-slate-700">
                              {route.status}
                            </span>
                          </div>
                          <div className="mt-1 truncate text-xs font-semibold opacity-80">{route.backend}</div>
                        </button>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </section>
          </aside>
        ) : null}

        <section className="relative min-w-0 flex-1">
          <ReactFlow<TopologyNode, TopologyEdge>
            key={`${namespace || 'none'}:${topologyMode}`}
            nodes={flowNodes}
            edges={flowEdges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
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

          {!graphReady ? (
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
