<script>
  import { onMount } from "svelte";
  import Card from "./lib/Card.svelte";
  import Toggle from "./lib/Toggle.svelte";
  import Segmented from "./lib/Segmented.svelte";
  import EQEditor from "./lib/EQEditor.svelte";
  import TouchPanel from "./lib/TouchPanel.svelte";
  import StatusBar from "./lib/StatusBar.svelte";
  import AdvancedPanel from "./lib/AdvancedPanel.svelte";
  import { LOCALES, locale, setLocale, t } from "./lib/i18n.js";

  import {
    Connect, Disconnect, Refresh, GetState,
    SetANCMode, SetANCLevel, SetTransparencyLevel,
    SetGameMode, SetLEAudio, SetTouchDisabled,
    SetVoicePack, SetPromptVolume,
    SetMultipoint, SetLDAC,
    SetKeyMappings,
    SetBatteryAlert, SetBatteryThreshold, TestBatteryNotification,
    SetLanguage,
    ApplyEQ, SendRaw,
  } from "../wailsjs/go/main/App";
  import { EventsOn } from "../wailsjs/runtime/runtime";

  // The device state pushed by Go. Not named `state`: next to the $state rune,
  // a variable of that name makes `$state` read as a store subscription.
  // Typed from the Wails-generated model, minus the conversion helper the
  // generator adds to every class: a plain object literal has no methods.
  /** @type {Omit<import("../wailsjs/go/models").main.State, "convertValues">} */
  let device = $state({
    connected: false, mac: "", channel: 0, firmware: "",
    batteryLeft: 0, batteryRight: 0, batteryCase: 0,
    ancMode: 0, ancLevel: 0, ancLevels: [],
    transparencyLevel: 0, transparencyLevels: [],
    gameMode: false, multipoint: false, ldac: false,
    leAudio: false, touchDisabled: false,
    promptVolume: 0, voicePack: 0,
    keyMappings: [], supported: {},
    batteryAlert: true, batteryThresh: 20,
    language: "en",
  });

  // No address is built in: Go remembers the last one that connected and
  // hands it over with the state.
  let mac = $state("");
  let tab = $state("audio");
  let busy = $state(false);
  let logs = $state([]);
  let frames = $state([]);
  /** Message from Go: { key, args }. Empty key means no error. */
  let connError = $state(/** @type {import("../wailsjs/go/models").main.Message | null} */ (null));
  let alert = $state(/** @type {{ labelKey: string, percent: number } | null} */ (null));

  let rawCmd = $state("0306");
  let rawPayload = $state("");
  let rawEqVendor = $state(false);

  const TABS = [
    { id: "audio", key: "tab.audio" },
    { id: "general", key: "tab.general" },
    { id: "touch", key: "tab.touch" },
    { id: "eq", key: "tab.eq" },
    { id: "advanced", key: "tab.advanced" },
  ];

  // Wire values paired with their catalogue key. The numbers are protocol
  // constants; only the label is translated.
  const ANC_MODES = [
    { value: 0, key: "anc.off" },
    { value: 1, key: "anc.on" },
    { value: 2, key: "anc.transparency" },
  ];

  const ANC_LEVEL_KEYS = {
    0x11: "ancLevel.adaptive", 0x12: "ancLevel.indoor",
    0x13: "ancLevel.outdoor", 0x14: "ancLevel.traffic",
  };
  const TRANSPARENCY_KEYS = {
    0x01: "transparency.voice", 0x02: "transparency.standard",
  };

  // Voice prompt languages are endonyms: they read the same whatever the
  // interface language is, so they stay out of the catalogue.
  const VOICE_PACKS = [
    { value: 0, label: "English" }, { value: 1, label: "中文" },
    { value: 2, label: "Deutsch" }, { value: 3, label: "日本語" },
    { value: 4, label: "Français" }, { value: 5, label: "Español" },
    { value: 6, label: "Italiano" },
  ];

  // Keep the Go side on the same language: desktop notifications must never
  // arrive in a different language from the window that triggered them.
  //
  // An effect rather than a plain call: it runs after the component is
  // mounted, so the Wails bindings are injected by the time it first fires,
  // and it fires again on every language change. The guard stays anyway — an
  // uncaught error here would leave a blank window, a far worse outcome than
  // notifications keeping the previous language.
  $effect(() => {
    const code = $locale;
    try {
      SetLanguage(code)?.catch(() => {});
    } catch {
      // Bindings not ready; the next language change reapplies it.
    }
  });

  const sup = $derived(device.supported || {});
  const ancModeOptions = $derived(
    ANC_MODES.map((m) => ({ value: m.value, label: $t(m.key) }))
  );
  const ancLevelOptions = $derived(
    (device.ancLevels || []).map((p) => ({
      value: p.code,
      label: ANC_LEVEL_KEYS[p.code]
        ? $t(ANC_LEVEL_KEYS[p.code])
        : `0x${p.code.toString(16)}`,
    }))
  );
  const transparencyOptions = $derived(
    (device.transparencyLevels || []).map((p) => ({
      value: p.code,
      label: TRANSPARENCY_KEYS[p.code]
        ? $t(TRANSPARENCY_KEYS[p.code])
        : `0x${p.code.toString(16)}`,
    }))
  );

  const ancLevelRelevant = $derived(device.ancMode === 1);
  const transparencyRelevant = $derived(device.ancMode === 2);

  const stamp = () => new Date().toLocaleTimeString($locale);

  onMount(async () => {
    EventsOn("state", (s) => { device = s; if (s.mac) mac = s.mac; });
    // Go sends { key, args } rather than text: the line is translated at
    // render time, so switching language re-renders the whole log.
    EventsOn("log", (message) => {
      logs = [...logs.slice(-260), { ts: stamp(), message }];
    });
    EventsOn("frame", (f) => { frames = [...frames.slice(-160), f]; });
    EventsOn("alert", (a) => {
      alert = a;
      setTimeout(() => { alert = null; }, 12000);
    });

    const s = await GetState();
    device = s;
    if (s.mac) mac = s.mac;
  });

  async function run(fn) {
    busy = true;
    try { await fn(); } finally { busy = false; }
  }

  const doConnect = () => run(async () => { connError = await Connect(mac); });
  const doDisconnect = () => run(Disconnect);
  const doRefresh = () => run(Refresh);

  const onApplyEQ = ({ bands, masterGain }) =>
    run(async () => { await ApplyEQ(bands, masterGain); });
  const onApplyKeys = (mappings) => run(async () => { await SetKeyMappings(mappings); });

  const sendRaw = ({ cmd, payload, eqVendor }) => run(async () => {
    const err = await SendRaw(cmd, payload, eqVendor);
    if (err && err.key) logs = [...logs, { ts: stamp(), message: err }];
  });
</script>

<header class="topbar">
  <div class="brand">
    <span class="dot" class:on={device.connected}></span>
    <strong>OpenPeats</strong>
    <span class="faint">{$t("app.tagline")}</span>
  </div>

  <div class="conn">
    <select class="lang" value={$locale} aria-label={$t("field.uiLanguage")}
            onchange={(e) => setLocale(e.currentTarget.value)}>
      {#each LOCALES as l}
        <option value={l.code}>{l.label}</option>
      {/each}
    </select>

    <input type="text" bind:value={mac} spellcheck="false" placeholder="AA:BB:CC:DD:EE:FF"
           disabled={busy || device.connected} aria-label={$t("field.macAddress")} />
    {#if device.connected}
      <button onclick={doRefresh} disabled={busy}>{$t("action.refresh")}</button>
      <button onclick={doDisconnect} disabled={busy}>{$t("action.disconnect")}</button>
    {:else}
      <button class="primary" onclick={doConnect} disabled={busy}>
        {busy ? $t("action.working") : $t("action.connect")}
      </button>
    {/if}
  </div>
</header>

{#if alert}
  <div class="alert">
    <strong>{$t("alert.lowBattery.title")}</strong> —
    {$t("alert.lowBattery.body", { label: $t(alert.labelKey), percent: alert.percent })}
    <button class="close" onclick={() => (alert = null)}
            aria-label={$t("action.dismiss")}>✕</button>
  </div>
{/if}

{#if connError && connError.key && !device.connected}
  <div class="err">{$t(connError)}</div>
{/if}

{#if device.connected}
  <StatusBar state={device} />
{/if}

<nav class="tabs">
  {#each TABS as item}
    <button class:active={tab === item.id} onclick={() => (tab = item.id)}>
      {$t(item.key)}
    </button>
  {/each}
</nav>

<main class="scroll">
  {#if !device.connected}
    <div class="empty-state">
      <h2>{$t("empty.title")}</h2>
      <p class="dim">{$t("empty.body")}</p>
    </div>

  {:else if tab === "audio"}
    <div class="grid">
      <Card title={$t("card.anc.title")} hint={$t("card.anc.hint")}>
        <Segmented options={ancModeOptions} value={device.ancMode} disabled={busy}
                   onchange={(v) => run(() => SetANCMode(v))} />
      </Card>

      <Card title={$t("card.ancLevel.title")}
            unsupported={sup.ancLevel === false}
            hint={ancLevelRelevant ? "" : $t("card.ancLevel.hint")}>
        <Segmented options={ancLevelOptions} value={device.ancLevel}
                   disabled={busy || !ancLevelRelevant}
                   onchange={(v) => run(() => SetANCLevel(v))} />
      </Card>

      <Card title={$t("card.transparency.title")}
            unsupported={sup.transparencyLevel === false}
            hint={transparencyRelevant ? "" : $t("card.transparency.hint")}>
        <Segmented options={transparencyOptions} value={device.transparencyLevel}
                   disabled={busy || !transparencyRelevant}
                   onchange={(v) => run(() => SetTransparencyLevel(v))} />
      </Card>

      <Card title={$t("card.codec.title")} hint={$t("card.codec.hint")}>
        <Toggle label={$t("toggle.ldac.label")} checked={device.ldac}
                disabled={busy || sup.ldac === false}
                note={$t("toggle.ldac.note")}
                onchange={(v) => run(() => SetLDAC(v))} />
        <Toggle label={$t("toggle.multipoint.label")} checked={device.multipoint}
                disabled={busy || sup.multipoint === false}
                note={$t("toggle.multipoint.note")}
                onchange={(v) => run(() => SetMultipoint(v))} />
        <Toggle label={$t("toggle.leAudio.label")} checked={device.leAudio}
                disabled={busy || sup.leAudio === false}
                onchange={(v) => run(() => SetLEAudio(v))} />
      </Card>
    </div>

  {:else if tab === "general"}
    <div class="grid">
      <Card title={$t("card.modes.title")}>
        <Toggle label={$t("toggle.gameMode.label")} checked={device.gameMode}
                disabled={busy || sup.gameMode === false}
                note={$t("toggle.gameMode.note")}
                onchange={(v) => run(() => SetGameMode(v))} />
        <Toggle label={$t("toggle.touchDisabled.label")}
                checked={device.touchDisabled}
                disabled={busy || sup.touchDisabled === false}
                note={$t("toggle.touchDisabled.note")}
                onchange={(v) => run(() => SetTouchDisabled(v))} />
      </Card>

      <Card title={$t("card.batteryAlert.title")} hint={$t("card.batteryAlert.hint")}>
        <Toggle label={$t("toggle.systemNotifications.label")}
                checked={device.batteryAlert} disabled={busy}
                onchange={(v) => run(() => SetBatteryAlert(v))} />
        <div class="field" style="margin-top:10px">
          <span class="faint">
            {$t("field.alertThreshold", { value: device.batteryThresh })}
          </span>
          <input type="range" min="5" max="50" step="5"
                 value={device.batteryThresh} disabled={busy}
                 onchange={(e) => run(() => SetBatteryThreshold(Number(e.currentTarget.value)))} />
        </div>
        <div style="margin-top:12px; display:flex; justify-content:flex-end">
          <button onclick={() => TestBatteryNotification()} disabled={busy}>
            {$t("action.testNotification")}
          </button>
        </div>
      </Card>

      <Card title={$t("card.voicePrompts.title")} unsupported={sup.voicePack === false}>
        <div class="field">
          <span class="faint">{$t("field.language")}</span>
          <select value={device.voicePack} disabled={busy}
                  onchange={(e) => run(() => SetVoicePack(Number(e.currentTarget.value)))}>
            {#each VOICE_PACKS as v}
              <option value={v.value}>{v.label}</option>
            {/each}
          </select>
        </div>
        <div class="field" style="margin-top:12px">
          <span class="faint">
            {$t("field.promptVolume", { value: device.promptVolume })}
          </span>
          <input type="range" min="0" max="100" step="1"
                 value={device.promptVolume} disabled={busy || sup.promptVolume === false}
                 onchange={(e) => run(() => SetPromptVolume(Number(e.currentTarget.value)))} />
        </div>
      </Card>

      <Card title={$t("card.missing.title")} hint={$t("card.missing.hint")}>
        <div class="chips">
          <!-- Go reports support per feature key (features.go); each one has
               a feature.<key> entry in the catalogue. -->
          {#each Object.entries(sup).filter(([, v]) => v === false) as [k]}
            <span class="chip">{$t(`feature.${k}`)}</span>
          {:else}
            <span class="faint">{$t("card.missing.none")}</span>
          {/each}
        </div>
      </Card>
    </div>

  {:else if tab === "touch"}
    <div class="single">
      <Card title={$t("card.touch.title")} unverified
            unsupported={sup.keyMappings === false}
            hint={$t("card.touch.hint")}>
        <TouchPanel mappings={device.keyMappings} {busy} onapply={onApplyKeys} />
      </Card>
    </div>

  {:else if tab === "eq"}
    <div class="single wide">
      <Card title={$t("card.eq.title")} hint={$t("card.eq.hint")}>
        <EQEditor {busy} onapply={onApplyEQ} />
      </Card>
    </div>

  {:else if tab === "advanced"}
    <AdvancedPanel {busy} {frames} {logs} onsend={sendRaw}
                   bind:rawCmd bind:rawPayload bind:eqVendor={rawEqVendor} />
  {/if}
</main>

<style>
  .topbar {
    display: flex; align-items: center; justify-content: space-between;
    gap: 16px; padding: 12px 20px;
    border-bottom: 1px solid var(--border); background: var(--bg-raised);
  }
  .brand { display: flex; align-items: center; gap: 9px; }
  .brand .faint { font-size: 0.8rem; }
  .dot {
    width: 9px; height: 9px; border-radius: 50%;
    background: var(--bad); box-shadow: 0 0 8px rgba(224, 94, 94, 0.5);
  }
  .dot.on { background: var(--good); box-shadow: 0 0 8px rgba(63, 191, 127, 0.5); }

  .conn { display: flex; gap: 8px; align-items: center; }
  .conn input { width: 172px; font-family: ui-monospace, monospace; font-size: 0.83rem; }
  /* Kept narrow: the language picker sits next to the connection controls
     without competing with them for attention. */
  .conn .lang { width: auto; padding: 6px 8px; font-size: 0.82rem; }

  .alert {
    margin: 12px 20px 0; padding: 10px 14px;
    background: #3a2410; border: 1px solid #7a4d15;
    color: #f6cf9a; border-radius: var(--radius-sm);
    font-size: 0.88rem; display: flex; align-items: center; gap: 8px;
  }
  .alert .close {
    margin-left: auto; border: none; background: none;
    color: inherit; padding: 2px 6px;
  }
  .alert .close:hover { background: rgba(255, 255, 255, 0.08); }

  .err {
    margin: 12px 20px 0; padding: 10px 13px;
    background: #2a1616; border: 1px solid #5c2626;
    color: #f0a8a8; border-radius: var(--radius-sm);
    font-size: 0.86rem; line-height: 1.5;
  }

  .tabs { display: flex; gap: 3px; padding: 10px 20px 0; }
  .tabs button {
    border: none; background: none; border-radius: 0;
    border-bottom: 2px solid transparent;
    padding: 8px 14px; color: var(--text-dim); font-size: 0.9rem;
  }
  .tabs button:hover { background: none; color: var(--text); }
  .tabs button.active {
    color: var(--text); border-bottom-color: var(--accent); font-weight: 700;
  }

  main { flex: 1; padding: 18px 20px 28px; }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(330px, 1fr));
    gap: 14px; align-items: start;
  }
  .single { display: flex; flex-direction: column; gap: 14px; max-width: 780px; }
  .single.wide { max-width: 900px; }

  .empty-state { max-width: 54ch; margin: 56px auto; text-align: center; }
  .empty-state h2 { font-size: 1.15rem; margin: 0 0 10px; }
  .empty-state p { font-size: 0.89rem; line-height: 1.6; margin: 0; }

  .field { display: flex; flex-direction: column; gap: 5px; font-size: 0.82rem; }

  .chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .chip {
    font-size: 0.76rem; padding: 3px 9px; border-radius: 20px;
    background: var(--bg-input); border: 1px solid var(--border-strong);
    color: var(--text-faint);
  }

</style>
