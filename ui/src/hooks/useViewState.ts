import { useCallback, useEffect, useState } from 'react';
import type { TopologyMode } from '../trafficGraph';
import { viewStateFromSearch, viewStateSearch, type ViewState } from '../uiState';

function initialViewState(): ViewState {
  if (typeof window === 'undefined') {
    return { namespace: '', search: '', route: '', mode: 'simple' };
  }
  return viewStateFromSearch(window.location.search);
}

export interface ViewStateControls {
  initialRoute: string;
  namespace: string;
  selectNamespace: (namespace: string) => void;
  routeSearch: string;
  setRouteSearch: (search: string) => void;
  selectedHostGroupId: string;
  selectHostGroup: (id: string) => void;
  topologyMode: TopologyMode;
  setTopologyMode: (mode: TopologyMode) => void;
}

// useViewState holds what a shared link restores and keeps it in the URL.
// The selected route is kept even while it is filtered out or not received
// yet, so a shared link or a cleared search brings it back.
export function useViewState(): ViewStateControls {
  const [initial] = useState(initialViewState);
  const [namespace, setNamespace] = useState(initial.namespace);
  const [routeSearch, setRouteSearch] = useState(initial.search);
  const [selectedHostGroupId, selectHostGroup] = useState(initial.route);
  const [topologyMode, setTopologyMode] = useState<TopologyMode>(initial.mode);

  const selectNamespace = useCallback((value: string) => {
    setNamespace(value);
    setRouteSearch('');
    selectHostGroup('');
  }, []);

  useEffect(() => {
    const search = viewStateSearch(
      { namespace, search: routeSearch, route: selectedHostGroupId, mode: topologyMode },
      window.location.search,
    );
    if (search !== window.location.search) {
      window.history.replaceState(window.history.state, '', `${window.location.pathname}${search}${window.location.hash}`);
    }
  }, [namespace, routeSearch, selectedHostGroupId, topologyMode]);

  return {
    initialRoute: initial.route,
    namespace,
    selectNamespace,
    routeSearch,
    setRouteSearch,
    selectedHostGroupId,
    selectHostGroup,
    topologyMode,
    setTopologyMode,
  };
}
