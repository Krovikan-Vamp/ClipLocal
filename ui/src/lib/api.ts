/**
 * Typed client for the ClipLocal Go backend REST/SSE endpoints.
 * All calls target localhost only – no external network requests.
 */

const BASE_URL = "http://localhost:8765";

export interface Clip {
  id: string;
  filepath: string;
  thumbnail_path: string;
  created_at: string;
  game_title: string;
  apm_at_capture: number;
  duration_seconds: number;
  size_bytes: number;
  shared: boolean;
}

export interface AppConfig {
  hotkey: string;
  obs: {
    path: string;
    port: number;
    password: string;
  };
  discord_webhook_url: string;
  replay_buffer_seconds: number;
  output_dir: string;
  max_storage_gb: number;
  port: number;
}

export interface TrimOptions {
  in_point: number;
  out_point: number;
}

async function request<T>(
  path: string,
  options?: RequestInit
): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`API ${path} → ${res.status}: ${text}`);
  }
  return res.json() as Promise<T>;
}

// ── Clips ──────────────────────────────────────────────────────────────────

export function listClips(): Promise<Clip[]> {
  return request<Clip[]>("/clips");
}

export function trimClip(id: string, opts: TrimOptions): Promise<Clip> {
  return request<Clip>(`/clips/${id}/trim`, {
    method: "POST",
    body: JSON.stringify(opts),
  });
}

export function shareClip(id: string): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>(`/clips/${id}/share`, { method: "POST" });
}

// ── Config ─────────────────────────────────────────────────────────────────

export function getConfig(): Promise<AppConfig> {
  return request<AppConfig>("/config");
}

export function putConfig(cfg: Partial<AppConfig>): Promise<AppConfig> {
  return request<AppConfig>("/config", {
    method: "PUT",
    body: JSON.stringify(cfg),
  });
}

// ── APM SSE stream ─────────────────────────────────────────────────────────

export interface APMEvent {
  apm: number;
}

/**
 * Subscribe to the live APM SSE stream.
 * Returns an EventSource; caller is responsible for closing it.
 */
export function openAPMStream(): EventSource {
  return new EventSource(`${BASE_URL}/apm/stream`);
}

// ── Health ─────────────────────────────────────────────────────────────────

export function health(): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>("/health");
}
