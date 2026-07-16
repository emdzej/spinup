// BFF auth client. The CP exposes /auth/me, /auth/login, /auth/logout;
// the login flow is entirely browser-driven (302 to IdP + cookie back), so we
// only need JSON round-trips for /auth/me and /auth/logout here.

export interface Me {
  sub?: string;
  email?: string;
  groups?: string[];
  roles?: string[];
  anonymous?: boolean;
  authorized: boolean;
}

// fetchMe returns status + parsed body. me is null when the response wasn't a
// well-formed /auth/me payload (CP down, proxy error, non-JSON). Callers must
// treat me==null as "cannot determine identity" and NOT as "not authorized".
export async function fetchMe(): Promise<{ status: number; me: Me | null }> {
  let res: Response;
  try {
    res = await fetch('/auth/me', { credentials: 'same-origin' });
  } catch {
    return { status: 0, me: null };
  }
  if (!res.ok) return { status: res.status, me: null };
  try {
    const me = (await res.json()) as Me;
    return { status: res.status, me };
  } catch {
    return { status: res.status, me: null };
  }
}

export function loginHref(returnTo: string = '/'): string {
  return `/auth/login?returnTo=${encodeURIComponent(returnTo)}`;
}

export async function logout(): Promise<{ endSessionUrl?: string }> {
  const res = await fetch('/auth/logout', { method: 'POST', credentials: 'same-origin' });
  if (!res.ok) throw new Error(`logout: ${res.status}`);
  // Dev-skip mode returns 204; real mode returns 200 with { endSessionUrl }.
  if (res.status === 204) return {};
  return (await res.json()) as { endSessionUrl?: string };
}
