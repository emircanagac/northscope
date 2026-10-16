import { useEffect, useState } from 'react';
import { SESSION_ENDPOINT, redirectToLogin, type SessionInfo } from '../auth';

// useSession reports whether built-in authentication is enabled and who is
// signed in. It returns null until the backend answers or if it cannot.
export function useSession(): SessionInfo | null {
  const [session, setSession] = useState<SessionInfo | null>(null);

  useEffect(() => {
    const controller = new AbortController();

    void fetch(SESSION_ENDPOINT, {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      signal: controller.signal,
    })
      .then((response) => {
        if (response.status === 401) {
          redirectToLogin();
          return null;
        }
        if (!response.ok) {
          throw new Error(`session request failed with ${response.status}`);
        }
        return response.json() as Promise<{ authEnabled?: unknown; username?: unknown }>;
      })
      .then((response) => {
        if (response && typeof response.authEnabled === 'boolean') {
          setSession({
            authEnabled: response.authEnabled,
            username: typeof response.username === 'string' ? response.username : undefined,
          });
        }
      })
      .catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === 'AbortError')) {
          setSession(null);
        }
      });

    return () => controller.abort();
  }, []);

  return session;
}
