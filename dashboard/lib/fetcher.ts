/**
 * SWR fetcher for the dashboard.
 *
 * Used as the default fetcher for useSWR. Receives the key (URL) and an opts
 * object that may contain a pre-built RequestInit. We forward the init to
 * fetch so callers can pass POST/PUT bodies, headers, etc.
 */
export const fetcher = async (url: string, init?: RequestInit) => {
  const res = await fetch(url, init);
  if (!res.ok) {
    const err = new Error(`Request failed (${res.status})`) as Error & {
      status?: number;
      info?: unknown;
    };
    err.status = res.status;
    try {
      err.info = await res.json();
    } catch {
      // ignore
    }
    throw err;
  }
  return res.json();
};
