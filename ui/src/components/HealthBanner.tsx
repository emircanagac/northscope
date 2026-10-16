import type { DegradedResource } from '../watchHealth';

function formatSince(since: string): string {
  const value = Date.parse(since);
  return Number.isNaN(value) || value <= 0 ? '' : ` since ${new Date(value).toLocaleTimeString()}`;
}

export function HealthBanner({ resources }: { resources: DegradedResource[] }) {
  if (resources.length === 0) {
    return null;
  }

  return (
    <div
      role="alert"
      data-testid="health-banner"
      className="shrink-0 border-b border-amber-200 bg-amber-50 px-4 py-2 text-amber-950 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-100"
    >
      <div className="text-xs font-black">Degraded: {resources.map((resource) => resource.resource).join(', ')}</div>
      <div className="mt-0.5 text-[11px] font-semibold text-amber-900 dark:text-amber-200">
        NorthScope cannot list or watch {resources.length === 1 ? 'this resource' : 'these resources'}, so the topology below may be stale or
        incomplete. Check read-only RBAC and API server connectivity.
      </div>
      <ul className="mt-1 max-h-24 space-y-0.5 overflow-auto">
        {resources.map((resource) => (
          <li key={resource.resource} title={resource.error} className="truncate font-mono text-[11px] text-amber-900 dark:text-amber-200">
            <span className="font-bold">{resource.resource}</span>
            {resource.synced ? '' : ' (not loaded)'}
            {formatSince(resource.since)}: {resource.error || 'unknown error'}
          </li>
        ))}
      </ul>
    </div>
  );
}
