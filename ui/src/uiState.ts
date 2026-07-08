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

  return [route.namespace, route.ingress, route.host, route.path, route.backend, route.status]
    .filter(Boolean)
    .some((value) => value.toLowerCase().includes(normalized));
}
