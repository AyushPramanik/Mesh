import type { Snapshot } from "../types";

// The daemon's dashboard API. Override with VITE_MESH_API when the daemon is
// not on the default local port.
export const API_BASE =
  import.meta.env.VITE_MESH_API ?? "http://localhost:7777";

export const SNAPSHOT_KEY = ["snapshot"] as const;

export async function fetchSnapshot(): Promise<Snapshot> {
  const res = await fetch(`${API_BASE}/api/snapshot`);
  if (!res.ok) {
    throw new Error(`snapshot request failed: ${res.status}`);
  }
  return res.json();
}
