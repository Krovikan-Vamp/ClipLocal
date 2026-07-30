import {
  createResource,
  createSignal,
  Show,
  Suspense,
} from "solid-js";
import { getConfig, putConfig, type AppConfig } from "~/lib/api";

function SettingsForm(props: {
  config: AppConfig;
  onSaved: () => void;
}) {
  const [hotkey, setHotkey] = createSignal(props.config.hotkey);
  const [webhookURL, setWebhookURL] = createSignal(
    props.config.discord_webhook_url
  );
  const [bufferSecs, setBufferSecs] = createSignal(
    props.config.replay_buffer_seconds
  );
  const [maxGB, setMaxGB] = createSignal(props.config.max_storage_gb);
  const [saving, setSaving] = createSignal(false);
  const [message, setMessage] = createSignal<string | null>(null);

  async function handleSave(e: Event) {
    e.preventDefault();
    setSaving(true);
    setMessage(null);
    try {
      await putConfig({
        hotkey: hotkey(),
        discord_webhook_url: webhookURL(),
        replay_buffer_seconds: bufferSecs(),
        max_storage_gb: maxGB(),
      });
      setMessage("Settings saved!");
      props.onSaved();
    } catch (err: unknown) {
      setMessage(
        `Error: ${err instanceof Error ? err.message : "Unknown error"}`
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <form class="settings-form" onSubmit={handleSave}>
      <div class="field-group">
        <label for="hotkey">Replay Hotkey</label>
        <input
          id="hotkey"
          type="text"
          value={hotkey()}
          onInput={(e) => setHotkey(e.currentTarget.value)}
          placeholder="e.g. F9"
          maxLength={20}
        />
        <small>Key name as shown in Windows (e.g. F9, F12)</small>
      </div>

      <div class="field-group">
        <label for="webhook">Discord Webhook URL</label>
        <input
          id="webhook"
          type="url"
          value={webhookURL()}
          onInput={(e) => setWebhookURL(e.currentTarget.value)}
          placeholder="https://discord.com/api/webhooks/..."
        />
        <small>
          Server Settings → Integrations → Webhooks → New Webhook
        </small>
      </div>

      <div class="field-group">
        <label for="buffer">Replay Buffer Length: {bufferSecs()}s</label>
        <input
          id="buffer"
          type="range"
          min={10}
          max={300}
          step={5}
          value={bufferSecs()}
          onInput={(e) =>
            setBufferSecs(parseInt(e.currentTarget.value, 10))
          }
        />
      </div>

      <div class="field-group">
        <label for="maxgb">Max Storage: {maxGB()} GB</label>
        <input
          id="maxgb"
          type="range"
          min={1}
          max={100}
          step={1}
          value={maxGB()}
          onInput={(e) =>
            setMaxGB(parseFloat(e.currentTarget.value))
          }
        />
        <small>Oldest clips are auto-deleted when limit is reached</small>
      </div>

      <Show when={message()}>
        <p
          class={`settings-msg ${
            message()?.startsWith("Error") ? "error" : "success"
          }`}
        >
          {message()}
        </p>
      </Show>

      <button type="submit" class="btn-save" disabled={saving()}>
        {saving() ? "Saving…" : "Save Settings"}
      </button>
    </form>
  );
}

export default function SettingsPage() {
  const [config, { refetch }] = createResource(getConfig);

  return (
    <main class="settings-page">
      <header>
        <h1>Settings</h1>
      </header>
      <Suspense fallback={<p class="loading">Loading settings…</p>}>
        <Show
          when={config()}
          fallback={<p class="error">Could not load settings. Is the backend running?</p>}
        >
          {(cfg) => <SettingsForm config={cfg()} onSaved={refetch} />}
        </Show>
      </Suspense>
    </main>
  );
}
