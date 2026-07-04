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
      const routeHostLaneId = ingressHostLaneId(namespace, ingress, host);
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
  const [selectedGroupId, setSelectedGroupId] = useState('');
  const [topologyMode, setTopologyMode] = useState<TopologyMode>('simple');
  const [flowInstance, setFlowInstance] = useState<ReactFlowInstance<TopologyNode, TopologyEdge> | null>(null);
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
  const selectedGroup = routeGroups.find((group) => group.id === selectedGroupId) ?? routeGroups[0];
  const selectedRouteIds = useMemo(() => selectedGroup?.routes.map((route) => route.id) ?? [], [selectedGroup]);
  const displayGraphNodes = useMemo(() => {
    if (topologyMode !== 'simple') {
      return visibleGraphNodes;
    }
    const selectedNodes = filterNodesByRoute(visibleGraphNodes, selectedRouteIds, selectedGroup?.id ?? '');
    return layoutTrafficPath(selectedNodes, topologyMode);
  }, [selectedGroup?.id, selectedRouteIds, topologyMode, visibleGraphNodes]);
  const displayGraphEdges = useMemo(() => {
    if (topologyMode !== 'simple') {
      return visibleGraphEdges;
    }
    return filterEdgesForNodes(visibleGraphEdges, displayGraphNodes);
  }, [displayGraphNodes, topologyMode, visibleGraphEdges]);
  const focusedGraphNodes = useMemo(
    () => focusNodesByRoute(displayGraphNodes, selectedRouteIds, selectedGroup?.id ?? ''),
    [displayGraphNodes, selectedGroup?.id, selectedRouteIds],
  );
  const focusedGraphEdges = useMemo(
    () => focusEdgesByRoute(displayGraphEdges, selectedRouteIds, selectedGroup?.id ?? ''),
    [displayGraphEdges, selectedGroup?.id, selectedRouteIds],
  );
  const hasSnapshot = Boolean(snapshot);
  const hasTopology = Boolean(snapshot && snapshot.nodes.length > 0);
  const graphReady = visibleGraphNodes.length > 0;
  const summary = useMemo(() => summarizeKinds(nodes), [nodes]);

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
    if (!flowInstance || flowNodes.length === 0) {
      return;
    }

    const frame = window.requestAnimationFrame(() => {
      void flowInstance.fitView({ padding: topologyMode === 'simple' ? 0.28 : 0.18, duration: 180 });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [flowEdges, flowInstance, flowNodes, selectedGroup?.id, topologyMode]);

  useEffect(() => {
    if (!routeGroups.some((group) => group.id === selectedGroupId)) {
      setSelectedGroupId(routeGroups[0]?.id ?? '');
    }
  }, [routeGroups, selectedGroupId]);

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-slate-100 text-slate-950">
      <header className="shrink-0 border-b border-slate-200 bg-white px-4 py-3 shadow-sm">
        <div className="flex min-h-10 flex-wrap items-center gap-3">
          <div className="mr-2 min-w-[210px]">
            <div className="text-sm font-black tracking-tight">NorthScope</div>
            <div className="text-[11px] font-medium text-slate-500">Kubernetes ingress traffic path debugger</div>
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
                  <button
                    key={group.id}
                    type="button"
                    onClick={() => setSelectedGroupId(group.id)}
                    className={routeGroupButtonClass(group, group.id === selectedGroup?.id)}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <div className="truncate text-sm font-black">{group.host}</div>
                        <div className="mt-0.5 truncate text-[11px] font-semibold opacity-70">{group.ingress}</div>
                      </div>
                      <span className="shrink-0 rounded-full bg-white/70 px-2 py-0.5 text-[10px] font-black uppercase text-slate-700">
                        {routeGroupStatus(group)}
                      </span>
                    </div>
                    <div className="mt-2 space-y-1.5">
                      {group.routes.map((route) => (
                        <div
                          key={route.id}
                          className="rounded border border-white/40 bg-white/45 px-2 py-1"
                        >
                          <div className="flex min-w-0 items-center gap-2">
                            <span className="min-w-0 truncate text-xs font-black">{route.path}</span>
                            <span className="shrink-0 text-[10px] font-bold uppercase opacity-70">{route.status}</span>
                          </div>
                          <div className="mt-0.5 truncate text-[11px] font-semibold opacity-75">{route.backend}</div>
                        </div>
                      ))}
                    </div>
                  </button>
                ))}
              </div>
            </section>
          </aside>
        ) : null}

        <section className="relative min-w-0 flex-1">
          <ReactFlow<TopologyNode, TopologyEdge>
            key={`${namespace || 'none'}:${topologyMode}:${selectedGroup?.id ?? 'none'}`}
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
