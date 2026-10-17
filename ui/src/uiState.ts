import type { RouteItem } from './trafficGraph';

export interface NamespaceOption {
  name: string;
  ingressCount: number;
}

export type NamespaceEscapeAction = 'clear' | 'restore';

export function namespaceInputValue(namespace: string, pickerOpen: boolean, namespaceQuery: string): string {
  return pickerOpen ? namespaceQuery : namespace || 'All namespaces';
}

export function isAllNamespacesQuery(query: string): boolean {
  const normalized = query.trim().toLowerCase();
  return !normalized || normalized === 'all' || normalized === 'all namespaces';
}

export function resolveNamespaceEnterSelection(query: string, options: NamespaceOption[]): string | null {
  if (isAllNamespacesQuery(query)) {
    return '';
  }

  return options[0]?.name ?? null;
}

export function namespaceEscapeAction(namespace: string, namespaceQuery: string, pickerOpen: boolean): NamespaceEscapeAction {
  if (!namespaceQuery.trim()) {
    return 'clear';
  }

  if (pickerOpen && namespaceQuery.trim() !== namespace) {
    return 'restore';
  }

  return 'clear';
}

export function routeMatchesSearch(route: RouteItem, query: string): boolean {
  const normalized = query.trim().toLowerCase();
  if (!normalized) {
    return true;
  }

  return [route.namespace, route.rootKind, route.ingress, route.host, route.path, route.backend, route.status, ...route.notes]
    .filter(Boolean)
    .some((value) => value.toLowerCase().includes(normalized));
}

export type ViewMode = 'simple' | 'expanded';

// ViewState is the part of the UI that a shared link restores: the namespace
// filter, the route search, the selected host route, and the layout mode.
export interface ViewState {
  namespace: string;
  search: string;
  route: string;
  mode: ViewMode;
}

const VIEW_PARAMS = {
  namespace: 'namespace',
  search: 'q',
  route: 'route',
  mode: 'view',
} as const;

export function viewStateFromSearch(search: string): ViewState {
  const params = new URLSearchParams(search);
  return {
    namespace: params.get(VIEW_PARAMS.namespace)?.trim() ?? '',
    search: params.get(VIEW_PARAMS.search) ?? '',
    route: params.get(VIEW_PARAMS.route) ?? '',
    mode: params.get(VIEW_PARAMS.mode) === 'expanded' ? 'expanded' : 'simple',
  };
}

// viewStateSearch writes the view into the query string, leaving unrelated
// parameters alone and omitting defaults so plain links stay short.
export function viewStateSearch(state: ViewState, currentSearch: string): string {
  const params = new URLSearchParams(currentSearch);
  const set = (key: string, value: string) => {
    if (value) {
      params.set(key, value);
    } else {
      params.delete(key);
    }
  };
  set(VIEW_PARAMS.namespace, state.namespace);
  set(VIEW_PARAMS.search, state.search.trim() ? state.search : '');
  set(VIEW_PARAMS.route, state.route);
  set(VIEW_PARAMS.mode, state.mode === 'expanded' ? 'expanded' : '');
  const query = params.toString();
  return query ? `?${query}` : '';
}
