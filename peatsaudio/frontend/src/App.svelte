<script>
  import { onMount } from "svelte";
  import Card from "./lib/Card.svelte";
  import Toggle from "./lib/Toggle.svelte";
  import Segmented from "./lib/Segmented.svelte";
  import EQEditor from "./lib/EQEditor.svelte";
  import TouchPanel from "./lib/TouchPanel.svelte";
  import { get } from "svelte/store";
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

  let state = {
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
  };

  let mac = "98:80:BB:40:D4:ED";
  let tab = "audio";
  let busy = false;
  let logs = [];
  let frames = [];
  /** Message from Go: { key, args }. Empty key means no error. */
  let connError = null;
  let alert = null;

  let rawCmd = "0306";
  let rawPayload = "";

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
  // Guarded rather than fire-and-forget: this runs during component setup,
  // and the Wails bindings throw synchronously if window.go is not injected
  // yet. An uncaught error there would leave a blank window — a far worse
  // outcome than notifications staying in the previous language.
  $: syncBackendLanguage($locale);

  function syncBackendLanguage(code) {
    try {
      SetLanguage(code)?.catch(() => {});
    } catch {
      // Bindings not ready. onMount below reapplies the language once they
      // are, so this only ever loses the very first attempt.
    }
  }

  $: sup = state.supported || {};
  $: ancModeOptions = ANC_MODES.map((m) => ({ value: m.value, label: $t(m.key) }));
  $: ancLevelOptions = (state.ancLevels || []).map((p) => ({
    value: p.code,
    label: ANC_LEVEL_KEYS[p.code]
      ? $t(ANC_LEVEL_KEYS[p.code])
      : `0x${p.code.toString(16)}`,
  }));
  $: transparencyOptions = (state.transparencyLevels || []).map((p) => ({
    value: p.code,
    label: TRANSPARENCY_KEYS[p.code]
      ? $t(TRANSPARENCY_KEYS[p.code])
      : `0x${p.code.toString(16)}`,
  }));

  $: ancLevelRelevant = state.ancMode === 1;
  $: transparencyRelevant = state.ancMode === 2;

  // Battery readings, paired with their label key so the status bar and the
  // low-battery banner name the same thing the same way.
  $: batteries = [
    { key: "battery.left", value: state.batteryLeft },
    { key: "battery.right", value: state.batteryRight },
    { key: "battery.case", value: state.batteryCase },
  ];

  const stamp = () => new Date().toLocaleTimeString($locale);

  onMount(async () => {
    EventsOn("state", (s) => { state = s; if (s.mac) mac = s.mac; });
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
    state = s;
    if (s.mac) mac = s.mac;

    // The bindings are certainly up by now; reapply in case the reactive
    // statement above ran before they were injected.
    syncBackendLanguage(get(locale));
  });

  async function run(fn) {
    busy = true;
    try { await fn(); } finally { busy = false; }
  }

  const doConnect = () => run(async () => { connError = await Connect(mac); });
  const doDisconnect = () => run(Disconnect);
  const doRefresh = () => run(Refresh);

  const onApplyEQ = (e) =>
    run(async () => { await ApplyEQ(e.detail.bands, e.detail.masterGain); });
  const onApplyKeys = (e) => run(async () => { await SetKeyMappings(e.detail); });

  const sendRaw = () => run(async () => {
    const err = await SendRaw(rawCmd, rawPayload, false);
    if (err && err.key) logs = [...logs, { ts: stamp(), message: err }];
  });

  function battClass(v) {
    if (v <= state.batteryThresh) return "bad";
    if (v <= state.batteryThresh + 15) return "warn";
    return "good";
  }
</script>

<header class="topbar">
  <div class="brand">
    <span class="dot" class:on={state.connected} />
    <strong>PeatsAudio</strong>
    <span class="faint">{$t("app.tagline")}</span>
  </div>

  <div class="conn">
    <select class="lang" value={$locale} aria-label={$t("field.uiLanguage")}
            on:change={(e) => setLocale(e.currentTarget.value)}>
      {#each LOCALES as l}
        <option value={l.code}>{l.label}</option>
      {/each}
    </select>

    <input type="text" bind:value={mac} spellcheck="false"
           disabled={busy || state.connected} aria-label={$t("field.macAddress")} />
    {#if state.connected}
      <button on:click={doRefresh} disabled={busy}>{$t("action.refresh")}</button>
      <button on:click={doDisconnect} disabled={busy}>{$t("action.disconnect")}</button>
    {:else}
      <button class="primary" on:click={doConnect} disabled={busy}>
        {busy ? $t("action.working") : $t("action.connect")}
      </button>
    {/if}
  </div>
</header>

{#if alert}
  <div class="alert">
    <strong>{$t("alert.lowBattery.title")}</strong> —
    {$t("alert.lowBattery.body", { label: $t(alert.labelKey), percent: alert.percent })}
    <button class="close" on:click={() => (alert = null)}
            aria-label={$t("action.dismiss")}>✕</button>
  </div>
{/if}

{#if connError && connError.key && !state.connected}
  <div class="err">{$t(connError)}</div>
{/if}

{#if state.connected}
  <div class="statusbar">
    <div class="batts">
      {#each batteries as b}
        <div class="batt">
          <span class="faint">{$t(b.key)}</span>
          <div class="bar">
            <div class="fill {battClass(b.value)}" style="width:{b.value}%" />
          </div>
          <span class="pct">{b.value}%</span>
        </div>
      {/each}
    </div>
    <div class="meta faint mono">
      {state.firmware || $t("status.firmwareUnknown")} ·
      {$t("status.channel", { channel: state.channel })}
    </div>
  </div>
{/if}

<nav class="tabs">
  {#each TABS as item}
    <button class:active={tab === item.id} on:click={() => (tab = item.id)}>
      {$t(item.key)}
    </button>
  {/each}
</nav>

<main class="scroll">
  {#if !state.connected}
    <div class="empty-state">
      <h2>{$t("empty.title")}</h2>
      <p class="dim">{$t("empty.body")}</p>
    </div>

  {:else if tab === "audio"}
    <div class="grid">
      <Card title={$t("card.anc.title")} hint={$t("card.anc.hint")}>
        <Segmented options={ancModeOptions} value={state.ancMode} disabled={busy}
                   on:change={(e) => run(() => SetANCMode(e.detail))} />
      </Card>

      <Card title={$t("card.ancLevel.title")}
            unsupported={sup.ancLevel === false}
            hint={ancLevelRelevant ? "" : $t("card.ancLevel.hint")}>
        <Segmented options={ancLevelOptions} value={state.ancLevel}
                   disabled={busy || !ancLevelRelevant}
                   on:change={(e) => run(() => SetANCLevel(e.detail))} />
      </Card>

      <Card title={$t("card.transparency.title")}
            unsupported={sup.transparencyLevel === false}
            hint={transparencyRelevant ? "" : $t("card.transparency.hint")}>
        <Segmented options={transparencyOptions} value={state.transparencyLevel}
                   disabled={busy || !transparencyRelevant}
                   on:change={(e) => run(() => SetTransparencyLevel(e.detail))} />
      </Card>

      <Card title={$t("card.codec.title")} hint={$t("card.codec.hint")}>
        <Toggle label={$t("toggle.ldac.label")} checked={state.ldac}
                disabled={busy || sup.ldac === false}
                note={$t("toggle.ldac.note")}
                on:change={(e) => run(() => SetLDAC(e.detail))} />
        <Toggle label={$t("toggle.multipoint.label")} checked={state.multipoint}
                disabled={busy || sup.multipoint === false}
                note={$t("toggle.multipoint.note")}
                on:change={(e) => run(() => SetMultipoint(e.detail))} />
        <Toggle label={$t("toggle.leAudio.label")} checked={state.leAudio}
                disabled={busy || sup.leAudio === false}
                on:change={(e) => run(() => SetLEAudio(e.detail))} />
      </Card>
    </div>

  {:else if tab === "general"}
    <div class="grid">
      <Card title={$t("card.modes.title")}>
        <Toggle label={$t("toggle.gameMode.label")} checked={state.gameMode}
                disabled={busy || sup.gameMode === false}
                note={$t("toggle.gameMode.note")}
                on:change={(e) => run(() => SetGameMode(e.detail))} />
        <Toggle label={$t("toggle.touchDisabled.label")}
                checked={state.touchDisabled}
                disabled={busy || sup.touchDisabled === false}
                note={$t("toggle.touchDisabled.note")}
                on:change={(e) => run(() => SetTouchDisabled(e.detail))} />
      </Card>

      <Card title={$t("card.batteryAlert.title")} hint={$t("card.batteryAlert.hint")}>
        <Toggle label={$t("toggle.systemNotifications.label")}
                checked={state.batteryAlert} disabled={busy}
                on:change={(e) => run(() => SetBatteryAlert(e.detail))} />
        <div class="field" style="margin-top:10px">
          <span class="faint">
            {$t("field.alertThreshold", { value: state.batteryThresh })}
          </span>
          <input type="range" min="5" max="50" step="5"
                 value={state.batteryThresh} disabled={busy}
                 on:change={(e) => run(() => SetBatteryThreshold(Number(e.currentTarget.value)))} />
        </div>
        <div style="margin-top:12px; display:flex; justify-content:flex-end">
          <button on:click={() => TestBatteryNotification()} disabled={busy}>
            {$t("action.testNotification")}
          </button>
        </div>
      </Card>

      <Card title={$t("card.voicePrompts.title")} unsupported={sup.voicePack === false}>
        <div class="field">
          <span class="faint">{$t("field.language")}</span>
          <select value={state.voicePack} disabled={busy}
                  on:change={(e) => run(() => SetVoicePack(Number(e.currentTarget.value)))}>
            {#each VOICE_PACKS as v}
              <option value={v.value}>{v.label}</option>
            {/each}
          </select>
        </div>
        <div class="field" style="margin-top:12px">
          <span class="faint">
            {$t("field.promptVolume", { value: state.promptVolume })}
          </span>
          <input type="range" min="0" max="100" step="1"
                 value={state.promptVolume} disabled={busy || sup.promptVolume === false}
                 on:change={(e) => run(() => SetPromptVolume(Number(e.currentTarget.value)))} />
        </div>
      </Card>

      <Card title={$t("card.missing.title")} hint={$t("card.missing.hint")}>
        <div class="chips">
          {#each Object.entries(sup).filter(([, v]) => v === false) as [k]}
            <span class="chip">{k}</span>
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
        <TouchPanel mappings={state.keyMappings} {busy} on:apply={onApplyKeys} />
      </Card>
    </div>

  {:else if tab === "eq"}
    <div class="single wide">
      <Card title={$t("card.eq.title")} hint={$t("card.eq.hint")}>
        <EQEditor {busy} on:apply={onApplyEQ} />
      </Card>
    </div>

  {:else if tab === "advanced"}
    <div class="single">
      <Card title={$t("card.raw.title")} hint={$t("card.raw.hint")}>
        <div class="raw">
          <label>
            <span class="faint">{$t("field.rawCommand")}</span>
            <input type="text" bind:value={rawCmd} placeholder="0306" spellcheck="false" />
          </label>
          <label>
            <span class="faint">{$t("field.rawPayload")}</span>
            <input type="text" bind:value={rawPayload} placeholder="01" spellcheck="false" />
          </label>
          <button on:click={sendRaw} disabled={busy}>{$t("action.send")}</button>
        </div>
      </Card>

      <Card title={$t("card.frames.title")} hint={$t("card.frames.hint")}>
        <div class="logs scroll">
          {#each frames.slice().reverse() as f}
            <div class="frameline">
              <span class="faint mono">{f.ts}</span>
              <span class="mono cmdcell">{f.cmd}</span>
              <span class="mono dim">{f.payload || "—"}</span>
            </div>
          {:else}
            <span class="faint">{$t("card.frames.empty")}</span>
          {/each}
        </div>
      </Card>

      <Card title={$t("card.log.title")}>
        <div class="logs scroll">
          {#each logs.slice().reverse() as l}
            <div class="logline">
              <span class="faint mono">{l.ts}</span>
              <span>{$t(l.message)}</span>
            </div>
          {:else}
            <span class="faint">{$t("card.log.empty")}</span>
          {/each}
        </div>
      </Card>
    </div>
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

  .statusbar {
    display: flex; align-items: center; justify-content: space-between;
    gap: 20px; padding: 11px 20px;
    border-bottom: 1px solid var(--border);
  }
  .batts { display: flex; gap: 22px; flex-wrap: wrap; }
  .batt { display: flex; align-items: center; gap: 8px; font-size: 0.8rem; }
  .bar {
    width: 74px; height: 7px; border-radius: 4px;
    background: var(--bg-input); overflow: hidden;
  }
  .fill { height: 100%; border-radius: 4px; transition: width 300ms; }
  .fill.good { background: var(--good); }
  .fill.warn { background: var(--warn); }
  .fill.bad { background: var(--bad); }
  .pct { min-width: 34px; }
  .meta { font-size: 0.76rem; }

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

  .raw { display: grid; grid-template-columns: 1fr 1fr auto; gap: 10px; align-items: end; }
  .raw label { display: flex; flex-direction: column; gap: 4px; font-size: 0.78rem; }

  .logs { height: 230px; font-size: 0.81rem; }
  .logline { display: flex; gap: 10px; padding: 2px 0; line-height: 1.45; }
  .frameline {
    display: grid; grid-template-columns: 92px 58px 1fr;
    gap: 10px; padding: 2px 0;
  }
  .cmdcell { color: var(--accent); font-weight: 700; }
</style>
