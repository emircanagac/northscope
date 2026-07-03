import { useEffect, useMemo, useState } from 'react';
import {
  Background,
  Controls,
  MiniMap,
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
  collectIngressSubgraphNodeIds,
  findControllerForIngress,
  isControllerNode,
  isIngressNode,
  layoutHorizontally,
  nodeDisplayName,
  nodeMatchesSearch,
  summarizeKinds,
  uniqueById,
} from './topologyView';

const nodeTypes = {
  northscopeNode: KubeNode,
} satisfies NodeTypes;

function statusLabel(status: string, hasSnapshot: boolean): string {
  if (status === 'connected') {
    if (!hasSnapshot) {
      return 'Syncing';
    }
    return 'Live';
  }
  if (status === 'connecting') {
    return 'Connecting';
  }
  return 'Reconnecting';
}

export default function App() {
  const { nodes, edges, snapshot, status, error } = useTopologyStream();
  const [search, setSearch] = useState('');
  const [namespace, setNamespace] = useState('');
  const [focusControllerId, setFocusControllerId] = useState('');
  const [focusIngressId, setFocusIngressId] = useState('');
  const [flowNodes, setNodes, onNodesChange] = useNodesState<TopologyNode>([]);
  const [flowEdges, setEdges, onEdgesChange] = useEdgesState<TopologyEdge>([]);

  const namespaces = useMemo(
    () =>
      Array.from(new Set(nodes.map((node) => node.data.namespace).filter((value): value is string => Boolean(value)))).sort(),
    [nodes],
  );

  const namespaceNodes = useMemo(
    () => (namespace ? nodes.filter((node) => node.data.namespace === namespace) : nodes),
    [namespace, nodes],
  );

  const namespaceIngressNodes = useMemo(
    () => namespaceNodes.filter(isIngressNode),
    [namespaceNodes],
  );

  const namespaceIngressIds = useMemo(() => new Set(namespaceIngressNodes.map((node) => node.id)), [namespaceIngressNodes]);

  const namespaceControllerIds = useMemo(() => {
    if (!namespace) {
      return [];
    }

    const controllerIds = new Set<string>();
    const nodeById = new Map(nodes.map((node) => [node.id, node]));

    for (const edge of edges) {
      if (String(edge.data?.kind ?? '').toLowerCase() !== 'controls') {
        continue;
      }

      if (!namespaceIngressIds.has(edge.target)) {
        continue;
      }

      const controllerNode = nodeById.get(edge.source);
      if (controllerNode && isControllerNode(controllerNode)) {
        controllerIds.add(controllerNode.id);
      }
    }

    return Array.from(controllerIds).sort((left, right) => left.localeCompare(right));
  }, [edges, namespace, namespaceIngressIds, nodes]);

  const namespaceControllerNodes = useMemo(
    () => namespaceControllerIds.map((id) => nodes.find((node) => node.id === id)).filter((node): node is TopologyNode => Boolean(node)),
    [namespaceControllerIds, nodes],
  );

  const ingressOptions = useMemo(
    () => {
      const query = search.trim().toLowerCase();
      const allowedControllerIngressIds = new Set<string>();

      if (focusControllerId) {
        for (const edge of edges) {
          if (edge.source !== focusControllerId) {
            continue;
          }
          if (String(edge.data?.kind ?? '').toLowerCase() !== 'controls') {
            continue;
          }
          allowedControllerIngressIds.add(edge.target);
        }
      }

      return nodes.filter((node) => {
        if (!isIngressNode(node)) {
          return false;
        }

        const matchesSearch = nodeMatchesSearch(node, query);
        const matchesNamespace = namespace === '' || node.data.namespace === namespace;
        const matchesController = !focusControllerId || allowedControllerIngressIds.has(node.id);
        return matchesSearch && matchesNamespace && matchesController;
      });
    },
    [edges, focusControllerId, namespace, nodes, search],
  );

  const uniqueSearchMatch = useMemo(() => {
    const query = search.trim();
    if (!query || ingressOptions.length !== 1) {
      return null;
    }

    return ingressOptions[0];
  }, [ingressOptions, search]);

  useEffect(() => {
    if (uniqueSearchMatch) {
      if (namespace !== (uniqueSearchMatch.data.namespace ?? '')) {
        setNamespace(uniqueSearchMatch.data.namespace ?? '');
      }

      if (focusIngressId !== uniqueSearchMatch.id) {
        setFocusIngressId(uniqueSearchMatch.id);
      }

      const controllerId = findControllerForIngress(uniqueSearchMatch.id, edges);
      if (controllerId && focusControllerId !== controllerId) {
        setFocusControllerId(controllerId);
      }
      return;
    }

    if (namespace === '') {
      setFocusControllerId('');
      setFocusIngressId('');
      return;
    }

    if (namespaceControllerIds.length === 1) {
      setFocusControllerId(namespaceControllerIds[0]);
    } else if (focusControllerId && !namespaceControllerIds.includes(focusControllerId)) {
      setFocusControllerId('');
    }

    if (focusControllerId && !namespaceControllerIds.includes(focusControllerId)) {
      setFocusIngressId('');
    }

    if (focusControllerId === '' && namespaceControllerIds.length > 1) {
      setFocusIngressId('');
    }

    if (ingressOptions.length === 1) {
      setFocusIngressId(ingressOptions[0].id);
      return;
    }

    if (focusIngressId && !ingressOptions.some((node) => node.id === focusIngressId)) {
      setFocusIngressId('');
    }
  }, [edges, focusControllerId, focusIngressId, ingressOptions, namespace, namespaceControllerIds, uniqueSearchMatch]);

  const focusedNodeIds = useMemo(() => {
    if (!focusIngressId) {
      return null;
    }

    return collectIngressSubgraphNodeIds(focusIngressId, nodes, edges);
  }, [edges, focusIngressId, nodes]);

  const hasActiveGraphFilter = namespace !== '' || search.trim() !== '' || focusIngressId !== '';

  const filteredNodeIds = useMemo(() => {
    if (!hasActiveGraphFilter) {
      return new Set<string>();
    }

    if (focusedNodeIds) {
      return focusedNodeIds;
    }

    const query = search.trim().toLowerCase();
    return new Set(
      nodes
        .filter((node) => namespace === '' || node.data.namespace === namespace || !node.data.namespace)
        .filter((node) => nodeMatchesSearch(node, query))
        .map((node) => node.id),
    );
  }, [focusedNodeIds, hasActiveGraphFilter, namespace, nodes, search]);

  const graphNodes = useMemo(() => {
    const sourceNodes = nodes.filter((node) => filteredNodeIds.has(node.id));

    return layoutHorizontally(uniqueById(sourceNodes)).map((node) => ({
      ...node,
      hidden: false,
    }));
  }, [filteredNodeIds, nodes]);

  const visibleNodeIds = useMemo(
    () => new Set(graphNodes.filter((node) => !node.hidden).map((node) => node.id)),
    [graphNodes],
  );

  const graphEdges = useMemo(() => {
    const sourceEdges = edges.filter((edge) => filteredNodeIds.has(edge.source) && filteredNodeIds.has(edge.target));

    return sourceEdges.map((edge) => {
      const highlighted = visibleNodeIds.has(edge.source) && visibleNodeIds.has(edge.target);

      return {
        ...edge,
        hidden: !highlighted,
        animated: edge.animated,
      };
    });
  }, [edges, filteredNodeIds, visibleNodeIds]);

  const visibleGraphNodes = useMemo(() => graphNodes.filter((node) => !node.hidden), [graphNodes]);
  const visibleGraphEdges = useMemo(() => graphEdges.filter((edge) => !edge.hidden), [graphEdges]);
  const clusterSummary = useMemo(() => summarizeKinds(nodes), [nodes]);
  const summary = useMemo(() => summarizeKinds(visibleGraphNodes), [visibleGraphNodes]);
  const hasSnapshot = Boolean(snapshot);
  const selectedIngress = useMemo(
    () => nodes.find((node) => node.id === focusIngressId) ?? null,
    [focusIngressId, nodes],
  );
  const hasTopology = Boolean(snapshot && snapshot.nodes.length > 0);
  const graphReady = visibleGraphNodes.length > 0;
  const searchQuery = search.trim();
  const searchStateLabel = hasSnapshot
    ? searchQuery
      ? `${visibleGraphNodes.length} matches`
      : `${nodes.length} total`
    : 'Waiting';
  const focusedLabel = selectedIngress ? `Focused: ${nodeDisplayName(selectedIngress)}` : 'Full topology';
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
    if (graphReady) {
      return `${visibleGraphNodes.length} nodes / ${visibleGraphEdges.length} edges`;
    }
    if (hasTopology && !hasActiveGraphFilter) {
      return 'Choose a namespace, search, or ingress focus to draw topology';
    }
    if (hasTopology) {
      return 'No topology matches filters';
    }
    return 'Snapshot received: 0 supported objects';
  }, [graphReady, hasActiveGraphFilter, hasTopology, snapshot, status, visibleGraphEdges.length, visibleGraphNodes.length]);
  const emptyStateTitle = useMemo(() => {
    if (!hasSnapshot) {
      return status === 'connected' ? 'Connected; waiting for first Kubernetes snapshot' : 'Waiting for topology stream';
    }
    if (hasTopology && !hasActiveGraphFilter) {
      return 'Choose what to inspect';
    }
    if (hasTopology) {
      return 'No topology matches filters';
    }
    return 'Snapshot received, but no supported topology objects were found';
  }, [hasActiveGraphFilter, hasSnapshot, hasTopology, status]);
  const emptyStateDescription = useMemo(() => {
    if (!hasSnapshot) {
      return 'If this stays here, check pod logs and read-only RBAC for ingresses, services, pods, endpointslices, and ingressclasses.';
    }
    if (hasTopology && !hasActiveGraphFilter) {
      return 'Select a namespace, search by host/service/pod/VIP, or focus an ingress to render a readable graph.';
    }
    if (hasTopology) {
      return 'Clear search, namespace, controller, or ingress focus to widen the graph.';
    }
    return 'The cluster may be empty for watched resources, or NorthScope may not have permission to list them.';
  }, [hasActiveGraphFilter, hasSnapshot, hasTopology]);

  useEffect(() => {
    setNodes(graphNodes);
    setEdges(graphEdges);
  }, [graphEdges, graphNodes, setEdges, setNodes]);

  return (
    <div className="h-screen w-screen bg-slate-50 text-slate-950">
      <div className="absolute left-0 right-0 top-0 z-10 border-b border-slate-200 bg-white/95 px-4 py-3 shadow-sm backdrop-blur">
        <div className="flex flex-wrap items-center gap-3">
          <div className="mr-2 min-w-[170px]">
            <div className="text-sm font-black tracking-tight">NorthScope</div>
            <div className="text-[11px] font-medium text-slate-500">{headerStatusText}</div>
            <div className="mt-1 flex max-w-[760px] flex-wrap gap-2 text-[10px] font-bold uppercase tracking-wide text-slate-500">
              <span className="rounded-full bg-slate-100 px-2 py-0.5 text-slate-700">DNS {clusterSummary.dns}</span>
              <span className="rounded-full bg-sky-50 px-2 py-0.5 text-sky-700">LB {clusterSummary.loadBalancers}</span>
              <span className="rounded-full bg-teal-50 px-2 py-0.5 text-teal-700">Gateway {clusterSummary.gateways}</span>
              <span className="rounded-full bg-cyan-50 px-2 py-0.5 text-cyan-700">Routes {clusterSummary.routes}</span>
              <span className="rounded-full bg-amber-50 px-2 py-0.5 text-amber-700">Controllers {clusterSummary.controllers}</span>
              <span className="rounded-full bg-blue-50 px-2 py-0.5 text-blue-700">Ingress {clusterSummary.ingresses}</span>
              <span className="rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700">Services {clusterSummary.services}</span>
              <span className="rounded-full bg-zinc-100 px-2 py-0.5 text-zinc-700">Nodes {clusterSummary.nodes}</span>
              <span className="rounded-full bg-violet-50 px-2 py-0.5 text-violet-700">Pods {clusterSummary.pods}</span>
            </div>
            {graphReady ? (
              <div className="mt-1 text-[10px] font-medium text-slate-400">
                {focusedLabel}: {summary.loadBalancers} LB / {summary.gateways} gateways / {summary.ingresses} ingresses / {summary.services} services / {summary.pods} pods
              </div>
            ) : null}
          </div>
          <select
            value={namespace}
            onChange={(event) => {
              setNamespace(event.target.value);
              setFocusIngressId('');
            }}
            className="h-9 rounded-lg border border-slate-300 bg-white px-3 text-sm font-medium outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
          >
            <option value="">Namespace (optional)</option>
            {namespaces.map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
          <select
            value={focusControllerId}
            onChange={(event) => {
              setFocusControllerId(event.target.value);
              setFocusIngressId('');
            }}
            disabled={namespace === '' || namespaceControllerNodes.length === 0}
            className="h-9 min-w-[280px] rounded-lg border border-slate-300 bg-white px-3 text-sm font-medium outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100 disabled:cursor-not-allowed disabled:bg-slate-100"
          >
            <option value="">
              {namespace === ''
                ? 'Controller (optional)'
                : namespaceControllerNodes.length === 0
                  ? 'No controller in namespace'
                  : 'Controller (optional)'}
            </option>
            {namespaceControllerNodes.map((item) => (
              <option key={item.id} value={item.id}>
                {nodeDisplayName(item)}
              </option>
            ))}
          </select>
          <select
            value={focusIngressId}
            onChange={(event) => setFocusIngressId(event.target.value)}
            disabled={ingressOptions.length === 0}
            className="h-9 min-w-[280px] rounded-lg border border-slate-300 bg-white px-3 text-sm font-medium outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100 disabled:cursor-not-allowed disabled:bg-slate-100"
          >
            <option value="">
              {ingressOptions.length === 0 ? 'No ingress matches' : 'Focus ingress (optional)'}
            </option>
            {ingressOptions.map((item) => (
              <option key={item.id} value={item.id}>
                {nodeDisplayName(item)}
              </option>
            ))}
          </select>
          <div className="relative min-w-[260px] flex-1">
            <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[11px] font-black uppercase tracking-wide text-slate-400">
              Find
            </span>
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              className="h-9 w-full rounded-lg border border-slate-300 bg-white pl-14 pr-24 text-sm outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100 disabled:cursor-not-allowed disabled:bg-slate-100"
              placeholder="host, namespace, service, pod, VIP"
              type="search"
              disabled={!hasSnapshot}
            />
            <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[11px] font-bold text-slate-400">
              {searchStateLabel}
            </span>
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
      </div>

      <ReactFlow<TopologyNode, TopologyEdge>
        key={`${focusIngressId || 'none'}:${namespace || 'none'}`}
        nodes={flowNodes}
        edges={flowEdges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        fitView
        fitViewOptions={{ padding: 0.24 }}
        className="pt-[124px]"
      >
        <Background color="#d4d7dd" gap={24} />
        <Controls position="bottom-right" />
        <MiniMap
          position="bottom-left"
          nodeColor={(node) => {
            const kind = String(node.data?.kind ?? '').toLowerCase();
            if (kind === 'controller') return '#d97706';
            if (kind === 'dns') return '#475569';
            if (kind === 'gateway') return '#0f766e';
            if (kind === 'route') return '#0891b2';
            if (kind === 'node') return '#52525b';
            if (kind === 'loadbalancer') return '#0284c7';
            if (kind === 'nodeport') return '#ea580c';
            if (kind === 'ingress') return '#2563eb';
            if (kind === 'service') return '#059669';
            if (kind === 'pod') return '#7c3aed';
            return '#64748b';
          }}
        />
        {error ? (
          <Panel position="top-right" className="mt-20 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs font-semibold text-red-700 shadow">
            {error}
          </Panel>
        ) : null}
      </ReactFlow>

      {!graphReady ? (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center pt-[124px]">
          <div className="rounded-2xl border border-dashed border-slate-300 bg-white/90 px-6 py-5 text-center shadow-sm">
            <div className="text-base font-bold text-slate-900">
              {emptyStateTitle}
            </div>
            <div className="mt-1 text-sm text-slate-500">
              {emptyStateDescription}
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
