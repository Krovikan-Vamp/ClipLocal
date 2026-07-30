import { createSignal, onCleanup, onMount } from "solid-js";
import { getConfig, openAPMStream } from "~/lib/api";

export default function OverlayPage() {
  const [apm, setApm] = createSignal(0);
  const [hotkey, setHotkey] = createSignal("F9");
  const [status, setStatus] = createSignal<"recording" | "idle" | "error">(
    "idle"
  );

  onMount(async () => {
    // Load hotkey from config
    try {
      const cfg = await getConfig();
      setHotkey(cfg.hotkey);
    } catch {
      // Backend might not be ready yet; ignore
    }

    // Subscribe to APM SSE stream
    const es = openAPMStream();

    es.addEventListener("apm", (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as { apm: number };
        setApm(data.apm);
        setStatus("recording");
      } catch {
        // ignore parse errors
      }
    });

    es.onerror = () => {
      setStatus("error");
    };

    onCleanup(() => es.close());
  });

  return (
    <div class="overlay">
      <span
        class={`status-dot ${status()}`}
        title={status() === "recording" ? "Recording" : status() === "error" ? "Backend offline" : "Idle"}
      />
      <span class="apm-value">{apm()}</span>
      <span class="apm-label">APM</span>
      <span class="hotkey-hint">{hotkey()}</span>
    </div>
  );
}
