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
  uniqueById,
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
  ingress: string;
  backend: string;
  status: string;
  severity: string;
  diagnosis: string;
  nextStep: string;
  kubectl: string;
  confidence: string;
}

interface NamespaceTrafficGraph {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
  routes: RouteItem[];
}

function statusLabel(status: string, hasSnapshot: boolean): string {
  if (status === 'connected') {
    return hasSnapshot ? 'Live' : 'Syncing';
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

function syntheticEdge(source: string, target: string, kind: string, label: string): TopologyEdge {
  return {
    id: `${source}->${target}:${kind}`,
    source,
    target,
    type: 'smoothstep',
    label,
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
      label: 'F5 / External edge',
      kind: 'F5',
      namespace,
      name: 'F5 edge',
      status: 'Assumed entry',
      properties: {
        role: 'visual traffic entry',
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

function layoutTrafficPath(nodes: TopologyNode[]): TopologyNode[] {
  const columnOrder = ['f5', 'controller', 'ingress', 'route', 'service', 'pod', 'node'];
  const columnX: Record<string, number> = {
    f5: 0,
    controller: 300,
    ingress: 620,
    route: 940,
    service: 1280,
    pod: 1620,
    node: 1960,
  };
  const columns = new Map<string, TopologyNode[]>();

  for (const node of nodes) {
    const kind = kindOf(node);
    const column = kind === 'f5' ? 'f5' : columnOrder.includes(kind) ? kind : 'route';
    columns.set(column, [...(columns.get(column) ?? []), node]);
  }

  return nodes.map((node) => {
    const kind = kindOf(node);
    const column = kind === 'f5' ? 'f5' : columnOrder.includes(kind) ? kind : 'route';
    const columnNodes = [...(columns.get(column) ?? [])].sort((left, right) =>
      nodeDisplayName(left).localeCompare(nodeDisplayName(right)),
    );
    const rowIndex = Math.max(0, columnNodes.findIndex((item) => item.id === node.id));

    return {
      ...node,
      position: {
        x: columnX[column],
        y: rowIndex * 230,
      },
    };
  });
}

function routeItemFromNode(route: TopologyNode, ingress: TopologyNode, service?: TopologyNode): RouteItem {
  const props = route.data.properties ?? {};
  return {
    id: route.id,
    ingressId: ingress.id,
    serviceId: service?.id ?? '',
    name: String(route.data.label ?? route.data.name),
    ingress: nodeDisplayName(ingress),
    backend: props.backend ?? (service ? nodeDisplayName(service) : 'missing service'),
    status: String(route.data.status ?? 'Unknown'),
    severity: props.severity ?? 'unknown',
    diagnosis: props.diagnosis ?? 'No diagnosis available yet.',
    nextStep: props.nextStep ?? 'Inspect the Kubernetes objects on this route.',
    kubectl: props.kubectl ?? '',
    confidence: props.confidence ?? 'Configured',
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

function downloadDebugBundle(namespace: string, route: RouteItem, snapshotVersion?: number): void {
  const bundle = {
    tool: 'northscope',
    generatedAt: new Date().toISOString(),
    snapshotVersion: snapshotVersion ?? null,
    namespace,
    mode: 'configured-path',
    route,
  };
  const blob = new Blob([JSON.stringify(bundle, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = `northscope-${namespace}-${route.name.replace(/[^a-z0-9.-]+/gi, '-').toLowerCase()}.json`;
  link.click();
  URL.revokeObjectURL(url);
}

function buildNamespaceTrafficGraph(
  namespace: string,
  nodes: TopologyNode[],
  edges: TopologyEdge[],
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
  const f5Node = namespaceTrafficNode(namespace);
  const fallbackController = syntheticControllerNode(namespace);
  let fallbackControllerUsed = false;

  graphNodes.set(f5Node.id, f5Node);

  const addNode = (node?: TopologyNode) => {
    if (node) {
      graphNodes.set(node.id, node);
    }
  };
  const addEdge = (edge: TopologyEdge) => {
    graphEdges.set(edge.id, edge);
  };

  for (const ingress of ingressNodes) {
    addNode(ingress);

    const controllerEdges = edges.filter((edge) => edge.target === ingress.id && edgeKind(edge) === 'controls');
    if (controllerEdges.length === 0) {
      fallbackControllerUsed = true;
      addEdge(syntheticEdge(f5Node.id, fallbackController.id, 'traffic', 'F5'));
      addEdge(syntheticEdge(fallbackController.id, ingress.id, 'controls', 'IngressClass'));
    }

    for (const controllerEdge of controllerEdges) {
      const controller = nodeById.get(controllerEdge.source);
      if (!controller || !isControllerNode(controller)) {
        continue;
      }
      addNode(controller);
      addEdge(syntheticEdge(f5Node.id, controllerEdge.source, 'traffic', 'F5'));
      addEdge(controllerEdge);
    }

    const routeEdges = edges.filter((edge) => edge.source === ingress.id && edgeKind(edge) === 'defines');
    for (const routeEdge of routeEdges) {
      const route = nodeById.get(routeEdge.target);
      if (kindOf(route) !== 'route' || route?.data.namespace !== namespace) {
        continue;
      }
      addNode(route);
      addEdge(routeEdge);

      const serviceEdge = edges.find((edge) => edge.source === route.id && edgeKind(edge) === 'routes');
      const service = serviceEdge ? nodeById.get(serviceEdge.target) : undefined;
      const displayService = service ?? syntheticMissingServiceNode(namespace, route);
      addNode(displayService);
      if (serviceEdge && service) {
        addEdge(serviceEdge);
      } else {
        addEdge(syntheticEdge(route.id, displayService.id, 'missing', 'missing'));
      }
      routeItems.push(routeItemFromNode(route, ingress, displayService));

      const podEdges = edges.filter(
        (edge) => edge.source === displayService.id && ['selects', 'endpointslice'].includes(edgeKind(edge)),
      );
      for (const podEdge of podEdges) {
        const pod = nodeById.get(podEdge.target);
        if (kindOf(pod) !== 'pod' || pod?.data.namespace !== namespace) {
          continue;
        }
        addNode(pod);
        addEdge(podEdge);

        const nodeHostEdges = edges.filter((edge) => edge.target === pod.id && edgeKind(edge) === 'hosts');
        for (const nodeHostEdge of nodeHostEdges) {
          const node = nodeById.get(nodeHostEdge.source);
          if (!node || kindOf(node) !== 'node') {
            continue;
          }
          addNode(node);
          addEdge(syntheticEdge(pod.id, node.id, 'runs_on', 'Node'));
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

  if (fallbackControllerUsed) {
    addNode(fallbackController);
  }

  return {
    nodes: layoutTrafficPath(uniqueById(Array.from(graphNodes.values()))),
    edges: Array.from(graphEdges.values()),
    routes: routeItems.sort((left, right) => left.name.localeCompare(right.name)),
  };
}

export default function App() {
  const { nodes, edges, snapshot, status, error } = useTopologyStream();
  const [namespace, setNamespace] = useState('');
  const [selectedRouteId, setSelectedRouteId] = useState('');
  const [flowNodes, setNodes, onNodesChange] = useNodesState<TopologyNode>([]);
  const [flowEdges, setEdges, onEdgesChange] = useEdgesState<TopologyEdge>([]);

  const namespaces = useMemo(
    () =>
      Array.from(new Set(nodes.map((node) => node.data.namespace).filter((value): value is string => Boolean(value)))).sort(),
    [nodes],
  );

  const namespaceGraph = useMemo(
    () => buildNamespaceTrafficGraph(namespace, nodes, edges),
    [edges, namespace, nodes],
  );

  const visibleGraphNodes = namespaceGraph.nodes;
  const visibleGraphEdges = namespaceGraph.edges;
  const routes = namespaceGraph.routes;
  const selectedRoute = routes.find((route) => route.id === selectedRouteId) ?? routes[0];
  const hasSnapshot = Boolean(snapshot);
  const hasTopology = Boolean(snapshot && snapshot.nodes.length > 0);
  const graphReady = visibleGraphNodes.length > 0;
  const summary = useMemo(() => summarizeKinds(nodes), [nodes]);

  const headerStatusText = useMemo(() => {
    if (!snapshot) {
      if (status === 'connected') {
        return 'Connected; syncing Kubernetes cache';
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
      return `${routes.length} ingress routes / ${visibleGraphNodes.length - 1} resources in ${namespace}`;
    }
    if (hasTopology) {
      return `No ingress traffic paths found in ${namespace}`;
    }
    return 'Snapshot received: 0 supported objects';
  }, [graphReady, hasTopology, namespace, routes.length, snapshot, status, visibleGraphNodes.length]);

  const emptyStateTitle = useMemo(() => {
    if (!hasSnapshot) {
      return status === 'connected' ? 'Connected; waiting for first Kubernetes snapshot' : 'Waiting for topology stream';
    }
    if (!namespace) {
      return 'Select a namespace';
    }
    if (hasTopology) {
      return 'No ingress paths in this namespace';
    }
    return 'Snapshot received, but no supported topology objects were found';
  }, [hasSnapshot, hasTopology, namespace, status]);

  const emptyStateDescription = useMemo(() => {
    if (!hasSnapshot) {
      return 'If this stays here, check pod logs and read-only RBAC for ingresses, services, pods, endpointslices, and ingressclasses.';
    }
    if (!namespace) {
      return 'NorthScope draws F5 -> controller -> ingress -> service -> pod -> node paths for the selected namespace.';
    }
    if (hasTopology) {
      return 'The namespace may not have an Ingress, backend Service, or selected Pods yet.';
    }
    return 'The cluster may be empty for watched resources, or NorthScope may not have permission to list them.';
  }, [hasSnapshot, hasTopology, namespace]);

  useEffect(() => {
    setNodes(visibleGraphNodes);
    setEdges(visibleGraphEdges);
  }, [setEdges, setNodes, visibleGraphEdges, visibleGraphNodes]);

  useEffect(() => {
    if (!routes.some((route) => route.id === selectedRouteId)) {
      setSelectedRouteId(routes[0]?.id ?? '');
    }
  }, [routes, selectedRouteId]);

  return (
    <div className="h-screen w-screen bg-slate-100 text-slate-950">
      <div className="absolute left-0 right-0 top-0 z-10 border-b border-slate-200 bg-white px-4 py-3 shadow-sm">
        <div className="flex flex-wrap items-center gap-3">
          <div className="mr-3 min-w-[210px]">
            <div className="text-sm font-black tracking-tight">NorthScope</div>
            <div className="text-[11px] font-medium text-slate-500">{headerStatusText}</div>
          </div>
          <select
            value={namespace}
            onChange={(event) => setNamespace(event.target.value)}
            className="h-9 min-w-[320px] rounded-md border border-slate-300 bg-white px-3 text-sm font-semibold outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
          >
            <option value="">Namespace</option>
            {namespaces.map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
          <div className="flex flex-wrap gap-2 text-[10px] font-bold uppercase tracking-wide text-slate-500">
            <span className="rounded-full bg-amber-50 px-2 py-0.5 text-amber-700">Controllers {summary.controllers}</span>
            <span className="rounded-full bg-blue-50 px-2 py-0.5 text-blue-700">Ingress {summary.ingresses}</span>
            <span className="rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700">Services {summary.services}</span>
            <span className="rounded-full bg-violet-50 px-2 py-0.5 text-violet-700">Pods {summary.pods}</span>
            <span className="rounded-full bg-zinc-100 px-2 py-0.5 text-zinc-700">Nodes {summary.nodes}</span>
          </div>
          <div
            className={`ml-auto rounded-full px-3 py-1 text-xs font-bold ${
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
      </div>

      {graphReady ? (
        <>
          <aside className="absolute bottom-4 left-4 top-[92px] z-10 flex w-[360px] flex-col rounded-md border border-slate-200 bg-white shadow-sm">
            <div className="border-b border-slate-200 px-4 py-3">
              <div className="text-xs font-black uppercase tracking-wide text-slate-500">Ingress routes</div>
              <div className="mt-1 text-sm font-semibold text-slate-900">{routes.length} configured path{routes.length === 1 ? '' : 's'}</div>
            </div>
            <div className="min-h-0 flex-1 space-y-2 overflow-auto p-3">
              {routes.map((route) => (
                <button
                  key={route.id}
                  type="button"
                  onClick={() => setSelectedRouteId(route.id)}
                  className={routeButtonClass(route, route.id === selectedRoute?.id)}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="min-w-0 truncate text-sm font-black">{route.name}</span>
                    <span className="shrink-0 rounded-full bg-white/70 px-2 py-0.5 text-[10px] font-black uppercase text-slate-700">
                      {route.status}
                    </span>
                  </div>
                  <div className="mt-1 truncate text-xs font-semibold opacity-80">{route.backend}</div>
                  <div className="mt-1 truncate text-[11px] opacity-70">{route.ingress}</div>
                </button>
              ))}
            </div>
          </aside>

          {selectedRoute ? (
            <aside className="absolute bottom-4 right-4 top-[92px] z-10 flex w-[380px] flex-col rounded-md border border-slate-200 bg-white shadow-sm">
              <div className="border-b border-slate-200 px-4 py-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate text-sm font-black text-slate-950">{selectedRoute.name}</div>
                    <div className="mt-1 truncate text-xs font-semibold text-slate-500">{selectedRoute.backend}</div>
                  </div>
                  <span className={`shrink-0 rounded-full border px-2 py-1 text-[10px] font-black uppercase ${severityClass(selectedRoute.severity)}`}>
                    {selectedRoute.status}
                  </span>
                </div>
              </div>
              <div className="min-h-0 flex-1 overflow-auto p-4">
                <div className={`rounded-md border p-3 text-sm font-semibold leading-5 ${severityClass(selectedRoute.severity)}`}>
                  {selectedRoute.diagnosis}
                </div>

                <div className="mt-4">
                  <div className="text-xs font-black uppercase tracking-wide text-slate-500">Look here first</div>
                  <p className="mt-2 text-sm font-medium leading-6 text-slate-700">{selectedRoute.nextStep}</p>
                </div>

                <div className="mt-4">
                  <div className="text-xs font-black uppercase tracking-wide text-slate-500">Suggested kubectl</div>
                  <pre className="mt-2 whitespace-pre-wrap break-words rounded-md bg-slate-950 p-3 text-xs font-semibold leading-5 text-slate-100">
                    {selectedRoute.kubectl || 'kubectl describe ingress ...'}
                  </pre>
                </div>

                <div className="mt-4 grid grid-cols-2 gap-2 text-xs font-semibold text-slate-600">
                  <div className="rounded-md bg-slate-100 px-3 py-2">
                    <div className="font-black uppercase text-slate-500">Mode</div>
                    <div className="mt-1 text-slate-900">Configured path</div>
                  </div>
                  <div className="rounded-md bg-slate-100 px-3 py-2">
                    <div className="font-black uppercase text-slate-500">Confidence</div>
                    <div className="mt-1 text-slate-900">{selectedRoute.confidence}</div>
                  </div>
                </div>

                <button
                  type="button"
                  onClick={() => downloadDebugBundle(namespace, selectedRoute, snapshot?.version)}
                  className="mt-4 w-full rounded-md bg-slate-900 px-3 py-2 text-sm font-black text-white transition hover:bg-slate-700"
                >
                  Download debug bundle
                </button>
              </div>
            </aside>
          ) : null}
        </>
      ) : null}

      <ReactFlow<TopologyNode, TopologyEdge>
        key={namespace || 'none'}
        nodes={flowNodes}
        edges={flowEdges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        fitView
        fitViewOptions={{ padding: 0.22 }}
        className="pt-[84px]"
      >
        <Background color="#d9dee8" gap={24} />
        <Controls position="bottom-right" />
        {error ? (
          <Panel position="top-right" className="mt-16 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs font-semibold text-red-700 shadow">
            {error}
          </Panel>
        ) : null}
      </ReactFlow>

      {!graphReady ? (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center pt-[84px]">
          <div className="rounded-md border border-dashed border-slate-300 bg-white/95 px-7 py-5 text-center shadow-sm">
            <div className="text-base font-bold text-slate-900">{emptyStateTitle}</div>
            <div className="mt-1 max-w-[560px] text-sm text-slate-500">{emptyStateDescription}</div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
