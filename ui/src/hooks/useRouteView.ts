import { useDeferredValue, useMemo } from 'react';
import { buildNamespaceTrafficGraph } from '../topologyGraph';
import {
  filterEdgesForNodes,
  filterNodesByRoute,
  focusEdgesByRoute,
  focusNodesByRoute,
  groupRoutes,
  groupRoutesByHost,
  layoutTrafficPath,
  type RouteGroup,
  type RouteHostGroup,
  type RouteItem,
  type TopologyMode,
} from '../trafficGraph';
import { routeMatchesSearch } from '../uiState';
import type { TopologyEdge, TopologyNode } from './useTopologyStream';

const NO_LANES: ReadonlySet<string> = new Set();

export interface RouteCounts {
  roots: number;
  hosts: number;
  paths: number;
}

export interface RouteView {
  routes: RouteItem[];
  filteredRoutes: RouteItem[];
  routeGroups: RouteGroup[];
  counts: RouteCounts;
  selectedHostGroup: RouteHostGroup | undefined;
  graphNodes: TopologyNode[];
  graphEdges: TopologyEdge[];
}

// useRouteView lists the routes of a namespace, filters them by the search,
// and draws only the selected host's lane: the list is cheap at any cluster
// size, while drawing every lane on each snapshot is not.
export function useRouteView(
  nodes: TopologyNode[],
  edges: TopologyEdge[],
  namespace: string,
  mode: TopologyMode,
  routeSearch: string,
  selectedHostGroupId: string,
): RouteView {
  // Typing in the search box stays responsive while thousands of routes are
  // filtered and regrouped in the background.
  const search = useDeferredValue(routeSearch);
  const routes = useMemo(
    () => buildNamespaceTrafficGraph(namespace, nodes, edges, mode, NO_LANES).routes,
    [edges, mode, namespace, nodes],
  );
  const filteredRoutes = useMemo(() => routes.filter((route) => routeMatchesSearch(route, search)), [routes, search]);
  const routeGroups = useMemo(() => groupRoutes(filteredRoutes), [filteredRoutes]);
  const hostGroups = useMemo(() => groupRoutesByHost(filteredRoutes), [filteredRoutes]);
  const counts = useMemo<RouteCounts>(
    () => ({
      roots: new Set(routes.map((route) => route.ingressId)).size,
      hosts: new Set(routes.map((route) => route.host)).size,
      paths: new Set(routes.map((route) => `${route.ingressId}:${route.host}:${route.path}`)).size,
    }),
    [routes],
  );
  const selectedHostGroup = useMemo(
    () => hostGroups.find((group) => group.id === selectedHostGroupId),
    [hostGroups, selectedHostGroupId],
  );

  const selectedLaneId = selectedHostGroup?.id ?? '';
  const lane = useMemo(
    () => (selectedLaneId ? buildNamespaceTrafficGraph(namespace, nodes, edges, mode, new Set([selectedLaneId])) : null),
    [edges, mode, namespace, nodes, selectedLaneId],
  );
  const selectedRouteIds = useMemo(
    () => Array.from(new Set(selectedHostGroup?.routes.map((route) => route.topologyId) ?? [])),
    [selectedHostGroup],
  );
  const selectedLaneIds = useMemo(
    () => Array.from(new Set(selectedHostGroup?.routes.map((route) => route.hostLaneId) ?? [])),
    [selectedHostGroup],
  );
  const graphNodes = useMemo(() => {
    if (!lane) {
      return [];
    }
    const laid = layoutTrafficPath(filterNodesByRoute(lane.nodes, selectedRouteIds, selectedLaneIds), mode);
    return focusNodesByRoute(laid, selectedRouteIds, selectedLaneIds);
  }, [lane, mode, selectedLaneIds, selectedRouteIds]);
  const graphEdges = useMemo(() => {
    if (!lane) {
      return [];
    }
    return focusEdgesByRoute(filterEdgesForNodes(lane.edges, graphNodes), selectedRouteIds, selectedLaneIds);
  }, [graphNodes, lane, selectedLaneIds, selectedRouteIds]);

  return { routes, filteredRoutes, routeGroups, counts, selectedHostGroup, graphNodes, graphEdges };
}
