import { TriangleExclamationPoint } from 'lucide-react';
import type { DegradedResource } from '../watchHealth';

function formatSince(since: string): string {
  const value = Date.parse(since);
  return Number.isNaN(value) || value <= 0 ? '' : ` since ${new Date(value).toLocaleTimeString()}`;
}

// HealthBanner is a Primer attention flash listing the watches that fail, so
// a stale topology is never mistaken for the current one.
export function HealthBanner({ resources }: { resources: DegradedResource[] }) {
  if (resources.length === 0) {
    return null;
  }

  return (
    <div role="alert" data-testid="health-banner" className="flex shrink-0 gap-3 border-b border-attention bg-attention-muted px-4 py-2.5 text-fg">
      <TriangleExclamationPoint aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0 text-attention" strokeWidth={2} />
      <div className="min-w-0 flex-1">
        <div className="text-[13px] font-semibold">Degraded: {resources.map((resource) => resource.resource).join(', ')}</div>
        <div className="text-[13px] text-fg-muted">
          NorthScope cannot list or watch {resources.length === 1 ? 'this resource' : 'these resources'}, so the topology below may be stale or
          incomplete. Check read-only RBAC and API server connectivity.
        </div>
        <ul className="mt-1 max-h-24 space-y-0.5 overflow-auto">
          {resources.map((resource) => (
            <li key={resource.resource} title={resource.error} className="truncate font-mono text-xs text-fg-muted">
              <span className="font-semibold text-fg">{resource.resource}</span>
              {resource.synced ? '' : ' (not loaded)'}
              {formatSince(resource.since)}: {resource.error || 'unknown error'}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
