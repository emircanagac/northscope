export type HealthStatus = 'ok' | 'degraded' | 'syncing';

export interface DegradedResource {
  resource: string;
  synced: boolean;
  since: string;
  error: string;
}

export interface SnapshotHealth {
  status: HealthStatus;
  degraded?: DegradedResource[];
}

export interface ResourceHealth {
  resource: string;
  optional?: boolean;
  synced: boolean;
  degraded: boolean;
  degradedSince?: string;
  lastSuccessAt?: string;
  lastError?: string;
  lastErrorAt?: string;
  errorCount: number;
}

export interface WatcherStatus {
  status: HealthStatus;
  ready: boolean;
  snapshotVersion: number;
  generatedAt?: string;
  degraded?: string[];
  unsynced?: string[];
  resources: ResourceHealth[];
}

export function degradedResources(health: SnapshotHealth | null | undefined): DegradedResource[] {
  return health?.status === 'degraded' ? (health.degraded ?? []) : [];
}

// healthFromStatus lets the UI explain a startup that never produces a first
// snapshot, for example when one resource cannot be listed.
export function healthFromStatus(status: WatcherStatus | null): SnapshotHealth | null {
  if (!status || !Array.isArray(status.resources)) {
    return null;
  }
  const degraded = status.resources
    .filter((resource) => resource.degraded)
    .map((resource) => ({
      resource: resource.resource,
      synced: resource.synced,
      since: resource.degradedSince ?? '',
      error: resource.lastError ?? '',
    }));
  return { status: degraded.length > 0 ? 'degraded' : status.status, degraded };
}

export function streamStatusLabel(status: string, hasSnapshot: boolean, degraded: boolean): string {
  if (status === 'connected') {
    if (!hasSnapshot) {
      return 'Syncing';
    }
    return degraded ? 'Degraded' : 'Live config';
  }
  if (status === 'connecting') {
    return 'Connecting';
  }
  return 'Reconnecting';
}

export function formatSnapshotAge(generatedAt: string, now: number): string {
  const generated = Date.parse(generatedAt);
  if (Number.isNaN(generated) || generated <= 0) {
    return '';
  }
  const seconds = Math.max(0, Math.floor((now - generated) / 1000));
  if (seconds < 10) {
    return 'just now';
  }
  if (seconds < 60) {
    return `${seconds}s ago`;
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m ago`;
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return `${hours}h ago`;
  }
  return `${Math.floor(hours / 24)}d ago`;
}
