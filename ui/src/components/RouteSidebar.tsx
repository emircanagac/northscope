import { Search, X } from 'lucide-react';
import { memo, useEffect, useRef } from 'react';
import { plural } from '../inventory';
import type { RouteCounts } from '../hooks/useRouteView';
import { routeGroupSeverity, routeGroupStatus, severityRank, type RouteGroup, type RouteItem } from '../trafficGraph';
import type { NamespaceOption } from '../uiState';
import { NamespacePicker } from './NamespacePicker';
import { RouteDetails } from './RouteDetails';
import { StateLabel, StatusIcon, cx, toneForSeverity } from './ui';

function worstSeverity(routes: RouteItem[]): string {
  return routes.reduce((worst, route) => (severityRank(route.severity) < severityRank(worst) ? route.severity : worst), routes[0]?.severity ?? 'unknown');
}

function hostLanes(group: RouteGroup): Array<[string, RouteItem[]]> {
  const lanes = new Map<string, RouteItem[]>();
  for (const route of group.routes) {
    const items = lanes.get(route.hostLaneId);
    if (items) {
      items.push(route);
    } else {
      lanes.set(route.hostLaneId, [route]);
    }
  }
  return Array.from(lanes);
}

const RouteGroupCard = memo(function RouteGroupCard({
  group,
  showNamespace,
  selectedHostGroupId,
  onSelectHost,
}: {
  group: RouteGroup;
  showNamespace: boolean;
  selectedHostGroupId: string;
  onSelectHost: (id: string) => void;
}) {
  const lanes = hostLanes(group);
  const paths = new Set(group.routes.map((route) => `${route.host}:${route.path}`)).size;
  const severity = routeGroupSeverity(group);
  const title = showNamespace ? `${group.namespace} / ${group.ingress}` : group.ingress;

  return (
    <li className="ns-route-group overflow-hidden rounded-md border border-line bg-surface">
      <div className="flex items-start gap-2 border-b border-line-muted bg-surface-muted px-3 py-2">
        <StatusIcon tone={toneForSeverity(severity)} className="mt-0.5" />
        <div className="min-w-0 flex-1">
          <h3 className="break-words text-[13px] font-semibold leading-snug text-fg">{title}</h3>
          <p className="text-xs text-fg-muted">
            {group.rootKind} · {plural(lanes.length, 'host')} / {plural(paths, 'path')}
          </p>
        </div>
        <StateLabel tone={toneForSeverity(severity)} className="mt-0.5 max-w-[110px]" title={routeGroupStatus(group)}>
          {routeGroupStatus(group)}
        </StateLabel>
      </div>
      <ul>
        {lanes.map(([laneId, routes]) => {
          const selected = laneId === selectedHostGroupId;
          const host = routes[0]?.host ?? 'unknown host';
          const hostSeverity = worstSeverity(routes);
          return (
            <li key={laneId} className="border-b border-line-muted last:border-b-0">
              <button
                type="button"
                data-testid="host-route"
                data-host={host}
                data-host-lane={laneId}
                aria-current={selected ? 'true' : undefined}
                onClick={() => onSelectHost(laneId)}
                className={cx(
                  'block w-full px-3 py-2 text-left transition-colors',
                  selected ? 'bg-surface-selected shadow-[inset_2px_0_0_var(--ns-accent)]' : 'hover:bg-surface-hover',
                )}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <StatusIcon tone={toneForSeverity(hostSeverity)} label={hostSeverity} className="h-3.5 w-3.5" />
                  <span className={cx('min-w-0 flex-1 break-all text-[13px] leading-snug', selected ? 'font-semibold text-fg' : 'text-fg')}>
                    {host}
                  </span>
                  {routes.length > 1 ? <span className="shrink-0 text-xs text-fg-muted">{plural(new Set(routes.map((route) => route.path)).size, 'path')}</span> : null}
                </span>
                <span className="mt-1 block space-y-1 pl-[22px]">
                  {routes.map((route) => (
                    <span key={route.id} className="block min-w-0">
                      <span className="flex min-w-0 items-baseline gap-2">
                        <span className="min-w-0 truncate font-mono text-xs text-fg">{route.path}</span>
                        <span className={cx('shrink-0 truncate text-xs', route.severity === 'ok' ? 'text-fg-muted' : toneForSeverity(route.severity) === 'danger' ? 'text-danger' : 'text-attention')}>
                          {route.status}
                        </span>
                      </span>
                      <span className="block break-words text-xs text-fg-muted">{route.backend}</span>
                      {route.notes.map((note) => (
                        <span key={note} className="block break-words text-xs text-attention">
                          {note}
                        </span>
                      ))}
                    </span>
                  ))}
                </span>
              </button>
              {selected ? <RouteDetails routes={routes} /> : null}
            </li>
          );
        })}
      </ul>
    </li>
  );
});

export interface RouteSidebarProps {
  namespace: string;
  namespaceOptions: NamespaceOption[];
  hasSnapshot: boolean;
  onSelectNamespace: (namespace: string) => void;
  routeSearch: string;
  onRouteSearchChange: (search: string) => void;
  counts: RouteCounts;
  routeGroups: RouteGroup[];
  hasRoutes: boolean;
  selectedHostGroupId: string;
  onSelectHost: (id: string) => void;
  initialRoute: string;
}

export function RouteSidebar({
  namespace,
  namespaceOptions,
  hasSnapshot,
  onSelectNamespace,
  routeSearch,
  onRouteSearchChange,
  counts,
  routeGroups,
  hasRoutes,
  selectedHostGroupId,
  onSelectHost,
  initialRoute,
}: RouteSidebarProps) {
  const scrolledToInitialRoute = useRef(false);
  const initialRouteListed =
    Boolean(initialRoute) && routeGroups.some((group) => group.routes.some((route) => route.hostLaneId === initialRoute));
  // A shared link may select a host far down the list; bring it into view
  // once, when it is first listed.
  useEffect(() => {
    if (!initialRouteListed || scrolledToInitialRoute.current) {
      return;
    }
    const frame = window.requestAnimationFrame(() => {
      scrolledToInitialRoute.current = true;
      document.querySelector(`[data-host-lane="${CSS.escape(initialRoute)}"]`)?.scrollIntoView({ block: 'nearest' });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [initialRoute, initialRouteListed]);

  return (
    <aside aria-label="Traffic routes" className="flex w-[336px] max-w-[48vw] shrink-0 flex-col border-r border-line bg-surface">
      <div className="shrink-0 border-b border-line-muted px-4 py-3">
        <NamespacePicker namespace={namespace} options={namespaceOptions} hasSnapshot={hasSnapshot} onSelect={onSelectNamespace} />
      </div>
      <div className="shrink-0 px-4 pb-2 pt-3">
        <div className="flex items-baseline justify-between gap-2">
          <h2 className="text-xs font-semibold text-fg">Traffic routes</h2>
          <span className="truncate text-xs text-fg-muted">
            {plural(counts.roots, 'root')} · {plural(counts.hosts, 'host')} · {plural(counts.paths, 'path')}
          </span>
        </div>
        <div className="relative mt-2">
          <Search aria-hidden="true" className="pointer-events-none absolute left-2.5 top-2 h-4 w-4 text-fg-muted" />
          <input
            type="search"
            aria-label="Search routes"
            value={routeSearch}
            onChange={(event) => onRouteSearchChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                onRouteSearchChange('');
              }
            }}
            placeholder="Search host, path, service, or resource"
            className="h-8 w-full rounded-md border border-line bg-surface-muted pl-8 pr-8 text-[13px] text-fg placeholder:text-fg-subtle focus:border-accent focus:bg-surface focus:outline-none focus:ring-1 focus:ring-accent [&::-webkit-search-cancel-button]:hidden"
          />
          {routeSearch ? (
            <button
              type="button"
              aria-label="Clear route search"
              onClick={() => onRouteSearchChange('')}
              className="absolute right-1 top-1 grid h-6 w-6 place-items-center rounded text-fg-muted hover:bg-surface-hover hover:text-fg"
            >
              <X aria-hidden="true" className="h-3.5 w-3.5" />
            </button>
          ) : null}
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto px-3 pb-3">
        {routeGroups.length > 0 ? (
          <ul className="space-y-2">
            {routeGroups.map((group) => (
              <RouteGroupCard
                key={group.id}
                group={group}
                showNamespace={!namespace}
                selectedHostGroupId={selectedHostGroupId}
                onSelectHost={onSelectHost}
              />
            ))}
          </ul>
        ) : (
          <p className="rounded-md border border-dashed border-line px-3 py-4 text-center text-[13px] text-fg-muted">
            {routeSearch && hasRoutes ? 'No routes match this search' : namespace ? 'No traffic routes in this namespace' : 'No traffic routes found'}
          </p>
        )}
      </div>
    </aside>
  );
}
