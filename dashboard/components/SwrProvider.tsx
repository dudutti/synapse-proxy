"use client";

import { ReactNode } from "react";
import { SWRConfig } from "swr";
import { fetcher } from "@/lib/fetcher";

/**
 * Global SWR config: keep the cache warm across client-side route
 * transitions. The dashboard fetches /api/keys and /api/analytics on
 * mount; without a shared cache, every navigation back to the home
 * page waits for the full Postgres round-trip before showing any
 * numbers. With this config, the last response is held in memory and
 * served instantly on remount, while a background revalidation keeps
 * the data fresh.
 */
export function SwrProvider({ children }: { children: ReactNode }) {
  return (
    <SWRConfig
      value={{
        fetcher,
        revalidateOnFocus: true,
        revalidateOnReconnect: true,
        revalidateIfStale: true,
        keepPreviousData: true,
        dedupingInterval: 2000,
      }}
    >
      {children}
    </SWRConfig>
  );
}
