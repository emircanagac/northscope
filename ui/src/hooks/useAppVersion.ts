import { useEffect, useState } from 'react';

interface VersionResponse {
  version?: unknown;
}

export function formatAppVersion(version: string): string {
  const value = version.trim();
  if (!value || value === 'dev' || value.startsWith('v')) {
    return value;
  }
  return `v${value}`;
}

export function useAppVersion(): string {
  const [version, setVersion] = useState('');

  useEffect(() => {
    const controller = new AbortController();

    void fetch('/api/version', {
      headers: { Accept: 'application/json' },
      signal: controller.signal,
    })
      .then((response) => {
        if (!response.ok) {
          throw new Error(`version request failed with ${response.status}`);
        }
        return response.json() as Promise<VersionResponse>;
      })
      .then((response) => {
        if (typeof response.version === 'string') {
          setVersion(formatAppVersion(response.version));
        }
      })
      .catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === 'AbortError')) {
          setVersion('');
        }
      });

    return () => controller.abort();
  }, []);

  return version;
}
