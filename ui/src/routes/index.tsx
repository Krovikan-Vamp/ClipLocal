import { createResource, createSignal, For, Show, Suspense } from "solid-js";
import { listClips, shareClip, type Clip } from "~/lib/api";

function formatBytes(bytes: number): string {
  const mb = bytes / (1024 * 1024);
  return `${mb.toFixed(1)} MB`;
}

function formatDuration(secs: number): string {
  const m = Math.floor(secs / 60);
  const s = Math.floor(secs % 60);
  return `${m}:${s.toString().padStart(2, "0")}`;
}

function ClipCard(props: { clip: Clip; onRefresh: () => void }) {
  const [sharing, setSharing] = createSignal(false);
  const [shared, setShared] = createSignal(props.clip.shared);
  const [error, setError] = createSignal<string | null>(null);

  async function handleShare() {
    if (shared() || sharing()) return;
    setSharing(true);
    setError(null);
    try {
      await shareClip(props.clip.id);
      setShared(true);
      props.onRefresh();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Share failed");
    } finally {
      setSharing(false);
    }
  }

  return (
    <div class="clip-card">
      <div class="clip-thumb">
        <img
          src={`http://localhost:8765/thumbs/${encodeURIComponent(
            props.clip.id
          )}`}
          alt={props.clip.game_title}
          loading="lazy"
        />
      </div>
      <div class="clip-info">
        <h3 class="clip-title">{props.clip.game_title || "Unknown Game"}</h3>
        <p class="clip-meta">
          {new Date(props.clip.created_at).toLocaleString()} &middot;{" "}
          {formatDuration(props.clip.duration_seconds)} &middot;{" "}
          {formatBytes(props.clip.size_bytes)}
        </p>
        <p class="clip-apm">APM at capture: {props.clip.apm_at_capture}</p>
        <Show when={error()}>
          <p class="clip-error">{error()}</p>
        </Show>
      </div>
      <div class="clip-actions">
        <button
          class={`btn-share ${shared() ? "shared" : ""}`}
          onClick={handleShare}
          disabled={shared() || sharing()}
        >
          {sharing() ? "Sharing…" : shared() ? "✓ Shared" : "Share to Discord"}
        </button>
      </div>
    </div>
  );
}

export default function IndexPage() {
  const [clips, { refetch }] = createResource(listClips);

  return (
    <main class="gallery">
      <header class="gallery-header">
        <h1>ClipLocal</h1>
        <button class="btn-refresh" onClick={() => refetch()}>
          Refresh
        </button>
      </header>

      <Suspense fallback={<p class="loading">Loading clips…</p>}>
        <Show
          when={(clips() ?? []).length > 0}
          fallback={
            <p class="empty">
              No clips yet. Press <kbd>F9</kbd> during gameplay to save one!
            </p>
          }
        >
          <div class="clip-grid">
            <For each={clips()}>
              {(clip) => <ClipCard clip={clip} onRefresh={refetch} />}
            </For>
          </div>
        </Show>
      </Suspense>
    </main>
  );
}
