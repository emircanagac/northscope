import { useEffect, useState } from 'react';
import type { WatcherStatus } from '../watchHealth';

// useWatcherStatus polls /api/status while active. The topology stream only
// carries health once a snapshot exists, so this explains a slow or blocked
// initial cache sync.
export function useWatcherStatus(active: boolean, intervalMs = 5000): WatcherStatus | null {
  const [status, setStatus] = useState<WatcherStatus | null>(null);

  useEffect(() => {
    if (!active) {
      return;
    }

    const controller = new AbortController();
    let timer: number | undefined;

    const load = () => {
      void fetch('/api/status', {
        cache: 'no-store',
        headers: { Accept: 'application/json' },
        signal: controller.signal,
      })
        .then((response) => {
          if (!response.ok) {
            throw new Error(`status request failed with ${response.status}`);
          }
          return response.json() as Promise<WatcherStatus>;
        })
        .then((response) => {
          setStatus(Array.isArray(response?.resources) ? response : null);
        })
        .catch((error: unknown) => {
          if (!(error instanceof DOMException && error.name === 'AbortError')) {
            setStatus(null);
          }
        })
        .finally(() => {
          if (!controller.signal.aborted) {
            timer = window.setTimeout(load, intervalMs);
          }
        });
    };

    load();

    return () => {
      controller.abort();
      if (timer !== undefined) {
        window.clearTimeout(timer);
      }
    };
  }, [active, intervalMs]);

  return status;
}
