import { useEffect, useMemo, useRef, useState } from 'react';
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
import { StableEdge } from './components/StableEdge';
import { reconcileTopologyEdges, reconcileTopologyNodes } from './flowState';
import {
  useTopologyStream,
  type TopologyEdge,
  type TopologyNode,
} from './hooks/useTopologyStream';
import { isIngressNode, summarizeKinds } from './topologyView';
import {
  filterEdgesForNodes,
  filterNodesByRoute,
  focusEdgesByRoute,
  focusNodesByRoute,
  groupRoutes,
  groupRoutesByHost,
  layoutTrafficPath,
  routeGroupButtonClass,
  routeGroupStatus,
  type RouteItem,
  type TopologyMode,
} from './trafficGraph';
import { buildNamespaceTrafficGraph } from './topologyGraph';
import {
  namespaceEscapeAction,
  namespaceInputValue,
  resolveNamespaceEnterSelection,
  routeMatchesSearch,
  type NamespaceOption,
} from './uiState';

const nodeTypes = {
  northscopeNode: KubeNode,
} satisfies NodeTypes;

const edgeTypes = {
  northscopeStep: StableEdge,
};

const THEME_STORAGE_KEY = 'northscope-theme';

type ThemeMode = 'light' | 'dark';

function initialThemeMode(): ThemeMode {
  if (typeof window === 'undefined') {
    return 'light';
  }
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
    if (stored === 'light' || stored === 'dark') {
      return stored;
    }
  } catch {
    return 'light';
  }
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
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

export default function App() {
  const { nodes, edges, snapshot, status, error } = useTopologyStream();
  const [namespace, setNamespace] = useState('');
  const [namespaceQuery, setNamespaceQuery] = useState('');
  const [namespacePickerOpen, setNamespacePickerOpen] = useState(false);
  const [routeSearch, setRouteSearch] = useState('');
  const [selectedHostGroupId, setSelectedHostGroupId] = useState('');
  const [topologyMode, setTopologyMode] = useState<TopologyMode>('simple');
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => initialThemeMode());
  const [flowInstance, setFlowInstance] = useState<ReactFlowInstance<TopologyNode, TopologyEdge> | null>(null);
  const lastFitViewSignature = useRef('');
  const lastGraphStateSignature = useRef('');
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
  const themedGraphEdges = useMemo(
    () =>
      focusedGraphEdges.map((edge) => ({
        ...edge,
        style: {
          ...(edge.style ?? {}),
          stroke: themeMode === 'dark' ? '#64748b' : '#94a3b8',
        },
        labelStyle: {
          ...(edge.labelStyle ?? {}),
          fill: themeMode === 'dark' ? '#cbd5e1' : '#334155',
        },
        labelBgStyle: {
          ...(edge.labelBgStyle ?? {}),
          fill: themeMode === 'dark' ? '#0f172a' : '#f8fafc',
          fillOpacity: 0.96,
        },
      })),
    [focusedGraphEdges, themeMode],
  );
  const hasSnapshot = Boolean(snapshot);
  const hasTopology = Boolean(snapshot && snapshot.nodes.length > 0);
  const topologySummary = useMemo(() => summarizeKinds(nodes), [nodes]);
  const summary = snapshot?.inventory ?? topologySummary;
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
      return 'If this stays here, check pod logs and read-only RBAC for ingresses, services, pods, endpointslices, endpoints, and ingressclasses.';
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
  const fitViewSignature = `${namespace || 'all'}:${topologyMode}:${selectedHostGroup?.id ?? 'none'}`;
  const isDarkMode = themeMode === 'dark';

  useEffect(() => {
    document.documentElement.classList.toggle('dark', isDarkMode);
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, themeMode);
    } catch {
      // Ignore storage errors so private browsing policies do not break the UI.
    }
  }, [isDarkMode, themeMode]);

  useEffect(() => {
    const resetGraph = lastGraphStateSignature.current !== fitViewSignature;
    lastGraphStateSignature.current = fitViewSignature;
    setNodes((current) => reconcileTopologyNodes(current, focusedGraphNodes, resetGraph));
    setEdges((current) => reconcileTopologyEdges(current, themedGraphEdges, resetGraph));
  }, [fitViewSignature, focusedGraphNodes, setEdges, setNodes, themedGraphEdges]);

  useEffect(() => {
    if (!selectedHostGroup) {
      lastFitViewSignature.current = '';
      return;
    }
    if (!flowInstance || flowNodes.length === 0) {
      return;
    }
    if (lastFitViewSignature.current === fitViewSignature) {
      return;
    }
    lastFitViewSignature.current = fitViewSignature;

    const frame = window.requestAnimationFrame(() => {
      void flowInstance.fitView({ padding: topologyMode === 'simple' ? 0.24 : 0.18, duration: 160 });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [fitViewSignature, flowInstance, flowNodes.length, selectedHostGroup, topologyMode]);

  useEffect(() => {
    if (selectedHostGroupId && !routeHostGroups.some((group) => group.id === selectedHostGroupId)) {
      setSelectedHostGroupId('');
    }
  }, [routeHostGroups, selectedHostGroupId]);

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-slate-100 text-slate-950 dark:bg-slate-950 dark:text-slate-100">
      <header className="shrink-0 border-b border-slate-200 bg-white px-4 py-3 shadow-sm dark:border-slate-800 dark:bg-slate-950 dark:shadow-none">
        <div className="flex min-h-10 flex-wrap items-center gap-3">
          <div className="mr-2 min-w-[210px]">
            <div className="text-sm font-black tracking-tight">NorthScope</div>
            <div className="text-[11px] font-medium text-slate-500 dark:text-slate-400">Kubernetes ingress traffic path debugger</div>
          </div>
          <div className="hidden min-w-0 flex-1 justify-end xl:flex">
            <div className="max-w-full truncate rounded-md border border-slate-200 bg-slate-50 px-3 py-1.5 text-[11px] font-semibold text-slate-500 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400">
              <span className="font-black uppercase tracking-wide text-slate-400 dark:text-slate-500">Cluster inventory</span>
              <span className="ml-2 normal-case tracking-normal text-slate-600 dark:text-slate-300">{clusterInventory}</span>
            </div>
          </div>
          <button
            type="button"
            onClick={() => setThemeMode(isDarkMode ? 'light' : 'dark')}
            aria-label={isDarkMode ? 'Switch to light theme' : 'Switch to dark theme'}
            className="ml-auto h-9 rounded-md border border-slate-300 bg-white px-3 text-xs font-black text-slate-600 shadow-sm transition hover:border-slate-400 hover:text-slate-950 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:border-slate-500 dark:hover:text-white xl:ml-0"
          >
            {isDarkMode ? 'Light' : 'Dark'}
          </button>
          <div className="flex h-9 overflow-hidden rounded-md border border-slate-300 bg-white p-0.5 text-xs font-black shadow-sm dark:border-slate-700 dark:bg-slate-900 dark:shadow-none">
            <button
              type="button"
              onClick={() => setTopologyMode('simple')}
              aria-pressed={topologyMode === 'simple'}
              className={`px-3 transition ${topologyMode === 'simple' ? 'rounded bg-slate-950 text-white shadow-sm dark:bg-white dark:text-slate-950' : 'text-slate-500 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white'}`}
            >
              Simple
            </button>
            <button
              type="button"
              onClick={() => setTopologyMode('expanded')}
              aria-pressed={topologyMode === 'expanded'}
              className={`px-3 transition ${topologyMode === 'expanded' ? 'rounded bg-slate-950 text-white shadow-sm dark:bg-white dark:text-slate-950' : 'text-slate-500 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white'}`}
            >
              Expanded
            </button>
          </div>
          <div
            data-testid="stream-status"
            className={`rounded-full px-3 py-1 text-xs font-bold ${
              status === 'connected' && hasSnapshot
                ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-200'
                : status === 'connecting' || (status === 'connected' && !hasSnapshot)
                  ? 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-200'
                  : 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-200'
            }`}
          >
            {statusLabel(status, hasSnapshot)}
          </div>
        </div>
      </header>

      <main className="flex min-h-0 flex-1">
        <aside className="flex w-[312px] max-w-[48vw] shrink-0 flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-950">
          <section className="shrink-0 border-b border-slate-100 px-4 py-3 dark:border-slate-800">
            <label className="text-[11px] font-black uppercase tracking-wide text-slate-500 dark:text-slate-400" htmlFor="namespace-search">
              Namespace
            </label>
            <div className="relative mt-1.5">
              <input
                id="namespace-search"
                value={namespaceInputValue(namespace, namespacePickerOpen, namespaceQuery)}
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
                    const selectedNamespace = resolveNamespaceEnterSelection(namespaceQuery, filteredNamespaceOptions);
                    if (selectedNamespace === '') {
                      clearNamespace();
                      return;
                    }
                    if (selectedNamespace) {
                      selectNamespace(selectedNamespace);
                    }
                  }
                  if (event.key === 'Escape') {
                    const action = namespaceEscapeAction(namespace, namespaceQuery, namespacePickerOpen);
                    if (action === 'clear') {
                      clearNamespace();
                    } else {
                      setNamespaceQuery(namespace);
                    }
                    setNamespacePickerOpen(false);
                  }
                }}
                className="h-9 w-full rounded-md border border-slate-300 bg-white px-3 pr-16 text-sm font-semibold outline-none transition focus:border-blue-500 focus:ring-2 focus:ring-blue-100 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-blue-500 dark:focus:ring-blue-950"
              />
              {namespace || namespaceQuery ? (
                <button
                  type="button"
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={clearNamespace}
                  className="absolute right-1.5 top-1.5 h-6 rounded px-2 text-[11px] font-black uppercase text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
                  aria-label="Clear namespace"
                >
                  Clear
                </button>
              ) : null}
              {namespacePickerOpen ? (
                <div className="absolute left-0 right-0 top-10 z-30 max-h-72 overflow-auto rounded-md border border-slate-200 bg-white py-1 shadow-lg dark:border-slate-700 dark:bg-slate-900 dark:shadow-none">
                  {!namespaceQuery.trim() || 'all namespaces'.includes(namespaceQuery.trim().toLowerCase()) ? (
                    <button
                      type="button"
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={clearNamespace}
                      className={`flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm font-semibold transition hover:bg-blue-50 dark:hover:bg-slate-800 ${
                        !namespace ? 'bg-blue-50 text-blue-800 dark:bg-blue-950 dark:text-blue-100' : 'text-slate-800 dark:text-slate-200'
                      }`}
                    >
                      <span className="min-w-0 truncate">All namespaces</span>
                      <span className="shrink-0 text-[10px] font-black uppercase text-slate-400 dark:text-slate-500">all</span>
                    </button>
                  ) : null}
                  {filteredNamespaceOptions.length > 0 ? (
                    filteredNamespaceOptions.map((option) => (
                      <button
                        key={option.name}
                        type="button"
                        onMouseDown={(event) => event.preventDefault()}
                        onClick={() => selectNamespace(option.name)}
                        className={`flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm font-semibold transition hover:bg-blue-50 dark:hover:bg-slate-800 ${
                          option.name === namespace ? 'bg-blue-50 text-blue-800 dark:bg-blue-950 dark:text-blue-100' : 'text-slate-800 dark:text-slate-200'
                        }`}
                      >
                        <span className="min-w-0 truncate">{option.name}</span>
                        <span className={`shrink-0 text-[10px] font-black uppercase ${option.ingressCount > 0 ? 'text-blue-600 dark:text-blue-300' : 'text-slate-400 dark:text-slate-500'}`}>
                          {option.ingressCount} ing
                        </span>
                      </button>
                    ))
                  ) : (
                    <div className="px-3 py-2 text-sm font-semibold text-slate-500 dark:text-slate-400">No namespace matches</div>
                  )}
                </div>
              ) : null}
            </div>
          </section>

          <section className="flex min-h-0 flex-1 flex-col">
            <div className="shrink-0 px-4 py-3">
              <div className="text-[11px] font-black uppercase tracking-wide text-slate-500 dark:text-slate-400">Ingress routes</div>
              <div className="mt-1 text-sm font-semibold text-slate-900 dark:text-slate-100">
                {routeIngressCount} Ingress Object{routeIngressCount === 1 ? '' : 's'}
              </div>
              <div className="mt-0.5 text-[11px] font-semibold text-slate-500 dark:text-slate-400">
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
                  className="h-8 w-full rounded-md border border-slate-200 bg-slate-50 px-3 pr-16 text-xs font-semibold outline-none transition placeholder:text-slate-400 focus:border-blue-500 focus:bg-white focus:ring-2 focus:ring-blue-100 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-blue-500 dark:focus:bg-slate-900 dark:focus:ring-blue-950"
                />
                {routeSearch ? (
                  <button
                    type="button"
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => setRouteSearch('')}
                    className="absolute right-1 top-1 h-6 rounded px-2 text-[10px] font-black uppercase text-slate-500 transition hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
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
                          <div className="break-words text-sm font-black leading-snug">{namespace ? group.ingress : `${group.namespace} / ${group.ingress}`}</div>
                          <div className="mt-0.5 text-[11px] font-semibold opacity-75">
                            {routesByHost.length} host{routesByHost.length === 1 ? '' : 's'} / {group.routes.length} path
                            {group.routes.length === 1 ? '' : 's'}
                          </div>
                        </div>
                        <span className="shrink-0 rounded-full bg-white/70 px-2 py-0.5 text-[10px] font-black uppercase text-slate-700 dark:bg-slate-950/70 dark:text-slate-200">
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
                              data-testid="host-route"
                              data-host={host}
                              onClick={() => setSelectedHostGroupId(hostLaneId)}
                              className={`w-full cursor-pointer space-y-1 rounded-md border-2 p-1.5 text-left shadow-sm transition ${
                                hostSelected
                                  ? 'border-slate-950 bg-white text-slate-950 ring-2 ring-blue-100 dark:border-blue-400 dark:bg-slate-900 dark:text-white dark:ring-blue-950'
                                  : 'border-slate-300 bg-white text-slate-900 hover:border-blue-400 hover:bg-white hover:ring-2 hover:ring-blue-100 dark:border-slate-700 dark:bg-slate-900/80 dark:text-slate-100 dark:hover:border-blue-500 dark:hover:bg-slate-900 dark:hover:ring-blue-950'
                              }`}
                            >
                              <div className="flex min-w-0 items-center justify-between gap-2 px-0.5">
                                <div className="min-w-0 break-words text-xs font-black leading-snug">{host}</div>
                                <div className="flex shrink-0 items-center gap-1">
                                  {hostRoutes.length > 1 ? (
                                    <span className="text-[10px] font-black uppercase text-slate-500 dark:text-slate-400">{hostRoutes.length} paths</span>
                                  ) : null}
                                  <span
                                    className={`rounded-full px-1.5 py-0.5 text-[9px] font-black uppercase ${
                                      hostSelected
                                        ? 'bg-slate-950 text-white dark:bg-white dark:text-slate-950'
                                        : 'border border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-200'
                                    }`}
                                  >
                                    {hostSelected ? 'Selected' : 'View'}
                                  </span>
                                </div>
                              </div>
                              {hostRoutes.map((route) => (
                                <div key={route.id} className={`rounded border px-2 py-1 ${hostSelected ? 'border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-950' : 'border-slate-100 bg-slate-50/80 dark:border-slate-800 dark:bg-slate-950/60'}`}>
                                  <div className="flex min-w-0 items-center gap-2">
                                    <span className="min-w-0 truncate text-xs font-black">{route.path}</span>
                                    <span className="shrink-0 text-[10px] font-bold uppercase opacity-70">{route.status}</span>
                                  </div>
                                  <div className="mt-0.5 break-words text-[11px] font-semibold leading-snug opacity-75">{route.backend}</div>
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
                <div className="rounded-md border border-dashed border-slate-200 bg-slate-50 px-3 py-4 text-sm font-semibold text-slate-500 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-400">
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

        <section className="relative min-w-0 flex-1 bg-slate-100 dark:bg-slate-950">
          <ReactFlow<TopologyNode, TopologyEdge>
            data-testid="topology-flow"
            nodes={flowNodes}
            edges={flowEdges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onInit={setFlowInstance}
          >
            <Background color={isDarkMode ? '#1e293b' : '#d9dee8'} gap={24} />
            <Controls position="bottom-right" />
            {error ? (
              <Panel position="top-right" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs font-semibold text-red-700 shadow dark:border-red-800 dark:bg-red-950 dark:text-red-100 dark:shadow-none">
                {error}
              </Panel>
            ) : null}
          </ReactFlow>

          {displayGraphNodes.length === 0 ? (
            <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
              <div className="rounded-md border border-dashed border-slate-300 bg-white/95 px-7 py-5 text-center shadow-sm dark:border-slate-700 dark:bg-slate-900/95 dark:shadow-none">
                <div className="text-base font-bold text-slate-900 dark:text-slate-100">{emptyStateTitle}</div>
                <div className="mt-1 max-w-[560px] text-sm text-slate-500 dark:text-slate-400">{emptyStateDescription}</div>
              </div>
            </div>
          ) : null}
        </section>
      </main>
    </div>
  );
}
