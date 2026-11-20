import { useMemo } from 'react';
import { AppHeader } from './components/AppHeader';
import { HealthBanner } from './components/HealthBanner';
import { RouteSidebar } from './components/RouteSidebar';
import { TopologyCanvas } from './components/TopologyCanvas';
import { emptyStateText } from './emptyState';
import { useAppVersion } from './hooks/useAppVersion';
import { useRouteView } from './hooks/useRouteView';
import { useSession } from './hooks/useSession';
import { useThemeMode } from './hooks/useThemeMode';
import { useTopologyStream } from './hooks/useTopologyStream';
import { useViewState } from './hooks/useViewState';
import { useWatcherStatus } from './hooks/useWatcherStatus';
import { inventoryParts, namespaceOptionsFromNodes } from './inventory';
import { summarizeKinds } from './topologyView';
import { degradedResources, healthFromStatus } from './watchHealth';

export default function App() {
  const { nodes, edges, snapshot, status, error } = useTopologyStream();
  const appVersion = useAppVersion();
  const session = useSession();
  const theme = useThemeMode();
  const view = useViewState();
  const routeView = useRouteView(nodes, edges, view.namespace, view.topologyMode, view.routeSearch, view.selectedHostGroupId);

  const hasSnapshot = Boolean(snapshot);
  const startupStatus = useWatcherStatus(status === 'connected' && !hasSnapshot);
  const degraded = useMemo(
    () => degradedResources(snapshot ? snapshot.health : healthFromStatus(startupStatus)),
    [snapshot, startupStatus],
  );
  const namespaceOptions = useMemo(() => namespaceOptionsFromNodes(nodes), [nodes]);
  const inventory = useMemo(() => {
    const summary = snapshot?.inventory ?? summarizeKinds(nodes);
    return `Cluster: ${inventoryParts(summary).join(' · ')}`;
  }, [nodes, snapshot?.inventory]);

  const { selectedHostGroup } = routeView;
  const emptyState =
    routeView.graphNodes.length > 0
      ? null
      : emptyStateText({
          hasSnapshot,
          connected: status === 'connected',
          hasTopology: Boolean(snapshot && snapshot.nodes.length > 0),
          hasRoutes: routeView.routes.length > 0,
          hasFilteredRoutes: routeView.filteredRoutes.length > 0,
          routeSelected: Boolean(selectedHostGroup),
          namespace: view.namespace,
          unsynced: startupStatus?.unsynced ?? [],
        });

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-canvas text-fg">
      <AppHeader
        appVersion={appVersion}
        inventory={inventory}
        topologyMode={view.topologyMode}
        onTopologyModeChange={view.setTopologyMode}
        routeSelected={Boolean(selectedHostGroup)}
        snapshot={snapshot}
        status={status}
        degraded={degraded.length > 0}
        isDark={theme.isDark}
        onToggleTheme={theme.toggle}
        session={session}
      />
      <HealthBanner resources={degraded} />
      <main className="flex min-h-0 flex-1">
        <RouteSidebar
          namespace={view.namespace}
          namespaceOptions={namespaceOptions}
          hasSnapshot={hasSnapshot}
          onSelectNamespace={view.selectNamespace}
          routeSearch={view.routeSearch}
          onRouteSearchChange={view.setRouteSearch}
          counts={routeView.counts}
          routeGroups={routeView.routeGroups}
          hasRoutes={routeView.routes.length > 0}
          selectedHostGroupId={selectedHostGroup?.id ?? ''}
          onSelectHost={view.selectHostGroup}
          initialRoute={view.initialRoute}
        />
        <TopologyCanvas
          nodes={routeView.graphNodes}
          edges={routeView.graphEdges}
          viewKey={`${view.namespace || 'all'}:${view.topologyMode}:${selectedHostGroup?.id ?? 'none'}`}
          mode={view.topologyMode}
          isDark={theme.isDark}
          error={error}
          emptyState={emptyState}
        />
      </main>
    </div>
  );
}
