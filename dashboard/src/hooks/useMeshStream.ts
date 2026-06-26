import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, SNAPSHOT_KEY, fetchSnapshot } from "../lib/api";
import type { Snapshot } from "../types";

// useMeshStream owns the single Server-Sent Events connection to the daemon
// (CLAUDE.md: one connection, managed here; components subscribe to slices via
// the hooks below and never open their own). Each snapshot pushed by the daemon
// is written into the React Query cache, so derived state lives in the cache
// rather than component state.
export function useMeshStream(): { connected: boolean } {
  const queryClient = useQueryClient();
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const source = new EventSource(`${API_BASE}/api/stream`);

    source.addEventListener("snapshot", (event) => {
      const snapshot = JSON.parse((event as MessageEvent).data) as Snapshot;
      queryClient.setQueryData(SNAPSHOT_KEY, snapshot);
    });
    source.onopen = () => setConnected(true);
    source.onerror = () => setConnected(false);

    return () => source.close();
  }, [queryClient]);

  return { connected };
}

// useSnapshot seeds the cache with an initial fetch; the stream keeps it fresh,
// so the query itself never refetches.
export function useSnapshot() {
  return useQuery({
    queryKey: SNAPSHOT_KEY,
    queryFn: fetchSnapshot,
    staleTime: Infinity,
  });
}

// Slice hooks: components subscribe to just the part of the stream they render.
export function useWorkspaces() {
  return useSnapshot().data?.workspaces ?? [];
}

export function usePRs() {
  return useSnapshot().data?.prs ?? [];
}

export function useTrains() {
  return useSnapshot().data?.trains ?? [];
}
