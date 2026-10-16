// Helpers for NorthScope's built-in sign-in. The backend answers 401 for API
// and WebSocket requests without a valid session; the UI then sends the
// browser to the server-rendered login page instead of retrying forever.

export const LOGIN_PATH = '/login';
export const LOGOUT_PATH = '/logout';
export const SESSION_ENDPOINT = '/api/session';

export interface SessionInfo {
  authEnabled: boolean;
  username?: string;
}

export type SessionCheck = 'authenticated' | 'unauthenticated' | 'unknown';

export function sessionCheckFromStatus(status: number): SessionCheck {
  if (status === 401) {
    return 'unauthenticated';
  }
  return status >= 200 && status < 300 ? 'authenticated' : 'unknown';
}

// loginRedirectPath returns the login URL that brings the user back to the
// current page after signing in.
export function loginRedirectPath(pathname: string, search: string): string {
  const target = `${pathname}${search}`;
  if (!target || target === '/' || pathname === LOGIN_PATH) {
    return LOGIN_PATH;
  }
  return `${LOGIN_PATH}?next=${encodeURIComponent(target)}`;
}

let redirecting = false;

export function redirectToLogin(): void {
  if (redirecting) {
    return;
  }
  redirecting = true;
  window.location.assign(loginRedirectPath(window.location.pathname, window.location.search));
}

// checkSession asks the backend whether the browser still has a session.
// Network errors and unexpected statuses are 'unknown' so a backend restart
// does not log the user out.
export async function checkSession(): Promise<SessionCheck> {
  try {
    const response = await fetch(SESSION_ENDPOINT, {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    });
    return sessionCheckFromStatus(response.status);
  } catch {
    return 'unknown';
  }
}
