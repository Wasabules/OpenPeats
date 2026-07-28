<script>
  import { onMount } from "svelte";
  import Card from "./lib/Card.svelte";
  import Toggle from "./lib/Toggle.svelte";
  import Segmented from "./lib/Segmented.svelte";
  import EQEditor from "./lib/EQEditor.svelte";
  import TouchPanel from "./lib/TouchPanel.svelte";

  import {
    Connect, Disconnect, Refresh, GetState,
    SetANCMode, SetANCLevel, SetTransparencyLevel,
    SetGameMode, SetLEAudio, SetTouchDisabled,
    SetVoicePack, SetPromptVolume,
    SetMultipoint, SetLDAC,
    SetKeyMappings,
    SetBatteryAlert, SetBatteryThreshold, TestBatteryNotification,
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
  };

  let mac = "98:80:BB:40:D4:ED";
  let tab = "audio";
  let busy = false;
  let logs = [];
  let frames = [];
  let connError = "";
  let alert = null;

  let rawCmd = "0306";
  let rawPayload = "";

  const TABS = [
    { id: "audio", label: "Audio" },
    { id: "general", label: "Général" },
    { id: "touch", label: "Tactile" },
    { id: "eq", label: "Égaliseur" },
    { id: "advanced", label: "Avancé" },
  ];

  const ANC_MODES = [
    { value: 0, label: "Arrêt" },
    { value: 1, label: "Réduction de bruit" },
    { value: 2, label: "Transparence" },
  ];

  const ANC_LEVEL_LABELS = {
    0x11: "Adaptative", 0x12: "Intérieur",
    0x13: "Extérieur", 0x14: "Trafic",
  };
  const TRANSPARENCY_LABELS = { 0x01: "Voix renforcée", 0x02: "Standard" };

  const VOICE_PACKS = [
    { value: 0, label: "English" }, { value: 1, label: "中文" },
    { value: 2, label: "Deutsch" }, { value: 3, label: "日本語" },
    { value: 4, label: "Français" }, { value: 5, label: "Español" },
    { value: 6, label: "Italiano" },
  ];

  $: sup = state.supported || {};
  $: ancLevelOptions = (state.ancLevels || []).map((p) => ({
    value: p.code,
    label: ANC_LEVEL_LABELS[p.code] || `0x${p.code.toString(16)}`,
  }));
  $: transparencyOptions = (state.transparencyLevels || []).map((p) => ({
    value: p.code,
    label: TRANSPARENCY_LABELS[p.code] || `0x${p.code.toString(16)}`,
  }));

  $: ancLevelRelevant = state.ancMode === 1;
  $: transparencyRelevant = state.ancMode === 2;

  onMount(async () => {
    EventsOn("state", (s) => { state = s; if (s.mac) mac = s.mac; });
    EventsOn("log", (line) => {
      logs = [...logs.slice(-260), { ts: new Date().toLocaleTimeString(), line }];
    });
    EventsOn("frame", (f) => { frames = [...frames.slice(-160), f]; });
    EventsOn("alert", (a) => {
      alert = a;
      setTimeout(() => { alert = null; }, 12000);
    });

    const s = await GetState();
    state = s;
    if (s.mac) mac = s.mac;
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
    if (err) logs = [...logs, { ts: new Date().toLocaleTimeString(), line: err }];
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
    <span class="faint">contrôle SoundPEATS sous Linux</span>
  </div>

  <div class="conn">
    <input type="text" bind:value={mac} spellcheck="false"
           disabled={busy || state.connected} aria-label="Adresse MAC" />
    {#if state.connected}
      <button on:click={doRefresh} disabled={busy}>Rafraîchir</button>
      <button on:click={doDisconnect} disabled={busy}>Déconnecter</button>
    {:else}
      <button class="primary" on:click={doConnect} disabled={busy}>
        {busy ? "…" : "Connecter"}
      </button>
    {/if}
  </div>
</header>

{#if alert}
  <div class="alert">
    <strong>Batterie faible</strong> — {alert.label} à {alert.pct} %.
    <button class="close" on:click={() => (alert = null)} aria-label="Fermer">✕</button>
  </div>
{/if}

{#if connError && !state.connected}
  <div class="err">{connError}</div>
{/if}

{#if state.connected}
  <div class="statusbar">
    <div class="batts">
      {#each [["Gauche", state.batteryLeft], ["Droite", state.batteryRight], ["Étui", state.batteryCase]] as [label, v]}
        <div class="batt">
          <span class="faint">{label}</span>
          <div class="bar"><div class="fill {battClass(v)}" style="width:{v}%" /></div>
          <span class="pct">{v}%</span>
        </div>
      {/each}
    </div>
    <div class="meta faint mono">
      {state.firmware || "firmware ?"} · canal RFCOMM {state.channel}
    </div>
  </div>
{/if}

<nav class="tabs">
  {#each TABS as t}
    <button class:active={tab === t.id} on:click={() => (tab = t.id)}>{t.label}</button>
  {/each}
</nav>

<main class="scroll">
  {#if !state.connected}
    <div class="empty-state">
      <h2>Écouteurs non connectés</h2>
      <p class="dim">
        Sortez les écouteurs de l'étui, puis connectez-vous. Si le multipoint est
        actif et que votre téléphone tient le canal de contrôle, coupez-y le
        Bluetooth au préalable.
      </p>
    </div>

  {:else if tab === "audio"}
    <div class="grid">
      <Card title="Réduction de bruit"
            hint="Le sous-mode ne s'applique qu'en réduction de bruit active.">
        <Segmented options={ANC_MODES} value={state.ancMode} disabled={busy}
                   on:change={(e) => run(() => SetANCMode(e.detail))} />
      </Card>

      <Card title="Sous-mode de réduction"
            unsupported={sup.ancLevel === false}
            hint={ancLevelRelevant ? "" : "Passez en réduction de bruit pour agir sur ce réglage."}>
        <Segmented options={ancLevelOptions} value={state.ancLevel}
                   disabled={busy || !ancLevelRelevant}
                   on:change={(e) => run(() => SetANCLevel(e.detail))} />
      </Card>

      <Card title="Niveau de transparence"
            unsupported={sup.transparencyLevel === false}
            hint={transparencyRelevant ? "" : "Passez en transparence pour agir sur ce réglage."}>
        <Segmented options={transparencyOptions} value={state.transparencyLevel}
                   disabled={busy || !transparencyRelevant}
                   on:change={(e) => run(() => SetTransparencyLevel(e.detail))} />
      </Card>

      <Card title="Codec et connectivité"
            hint="LDAC et multipoint sont exclusifs : le firmware coupe LDAC dès que le multipoint s'active, faute de bande passante pour deux liens.">
        <Toggle label="LDAC" checked={state.ldac} disabled={busy || sup.ldac === false}
                note="Qualité maximale, un seul appareil à la fois."
                on:change={(e) => run(() => SetLDAC(e.detail))} />
        <Toggle label="Multipoint (double appareil)" checked={state.multipoint}
                disabled={busy || sup.multipoint === false}
                note="PC et téléphone simultanément, en aptX."
                on:change={(e) => run(() => SetMultipoint(e.detail))} />
        <Toggle label="LE Audio" checked={state.leAudio}
                disabled={busy || sup.leAudio === false}
                on:change={(e) => run(() => SetLEAudio(e.detail))} />
      </Card>
    </div>

  {:else if tab === "general"}
    <div class="grid">
      <Card title="Modes">
        <Toggle label="Mode jeu" checked={state.gameMode}
                disabled={busy || sup.gameMode === false}
                note="Réduit la latence audio."
                on:change={(e) => run(() => SetGameMode(e.detail))} />
        <Toggle label="Désactiver les commandes tactiles"
                checked={state.touchDisabled}
                disabled={busy || sup.touchDisabled === false}
                note="Ignore tous les appuis sur les écouteurs."
                on:change={(e) => run(() => SetTouchDisabled(e.detail))} />
      </Card>

      <Card title="Alerte de batterie faible"
            hint="Les niveaux sont relus toutes les 90 secondes. Une notification de bureau est envoyée au franchissement du seuil, une seule fois par décharge.">
        <Toggle label="Notifications système" checked={state.batteryAlert}
                disabled={busy}
                on:change={(e) => run(() => SetBatteryAlert(e.detail))} />
        <div class="field" style="margin-top:10px">
          <span class="faint">Seuil d'alerte — {state.batteryThresh} %</span>
          <input type="range" min="5" max="50" step="5"
                 value={state.batteryThresh} disabled={busy}
                 on:change={(e) => run(() => SetBatteryThreshold(Number(e.currentTarget.value)))} />
        </div>
        <div style="margin-top:12px; display:flex; justify-content:flex-end">
          <button on:click={() => TestBatteryNotification()} disabled={busy}>
            Tester la notification
          </button>
        </div>
      </Card>

      <Card title="Invites vocales" unsupported={sup.voicePack === false}>
        <div class="field">
          <span class="faint">Langue</span>
          <select value={state.voicePack} disabled={busy}
                  on:change={(e) => run(() => SetVoicePack(Number(e.currentTarget.value)))}>
            {#each VOICE_PACKS as v}
              <option value={v.value}>{v.label}</option>
            {/each}
          </select>
        </div>
        <div class="field" style="margin-top:12px">
          <span class="faint">Volume des invites — {state.promptVolume}</span>
          <input type="range" min="0" max="100" step="1"
                 value={state.promptVolume} disabled={busy || sup.promptVolume === false}
                 on:change={(e) => run(() => SetPromptVolume(Number(e.currentTarget.value)))} />
        </div>
      </Card>

      <Card title="Fonctions absentes de ce modèle"
            hint="Détectées au rafraîchissement : ces commandes ne répondent pas sur vos écouteurs.">
        <div class="chips">
          {#each Object.entries(sup).filter(([, v]) => v === false) as [k]}
            <span class="chip">{k}</span>
          {:else}
            <span class="faint">Toutes les fonctions interrogées répondent.</span>
          {/each}
        </div>
      </Card>
    </div>

  {:else if tab === "touch"}
    <div class="single">
      <Card title="Commandes tactiles" unverified
            unsupported={sup.keyMappings === false}
            hint="Le mode démo de l'app Android a été testé ici : la commande est acquittée mais les appuis ne remontent jamais et continuent d'agir normalement. Le tactile est géré en local par le firmware — le moniteur live a donc été retiré.">
        <TouchPanel mappings={state.keyMappings} {busy} on:apply={onApplyKeys} />
      </Card>
    </div>

  {:else if tab === "eq"}
    <div class="single wide">
      <Card title="Égaliseur paramétrique"
            hint="Filtres peaking : réglez le gain au fader ou en faisant glisser un point de la courbe, et la largeur de bande avec le Q au-dessus de chaque fader.">
        <EQEditor {busy} on:apply={onApplyEQ} />
      </Card>
    </div>

  {:else if tab === "advanced"}
    <div class="single">
      <Card title="Trame manuelle"
            hint="Envoie une commande arbitraire. Aucune réémission : une commande inconnue peut avoir un effet de bord irréversible.">
        <div class="raw">
          <label>
            <span class="faint">Commande (2 octets hex)</span>
            <input type="text" bind:value={rawCmd} placeholder="0306" spellcheck="false" />
          </label>
          <label>
            <span class="faint">Payload (hex, optionnel)</span>
            <input type="text" bind:value={rawPayload} placeholder="01" spellcheck="false" />
          </label>
          <button on:click={sendRaw} disabled={busy}>Envoyer</button>
        </div>
      </Card>

      <Card title="Trames reçues"
            hint="Toutes les trames remontées par les écouteurs, y compris spontanées.">
        <div class="logs scroll">
          {#each frames.slice().reverse() as f}
            <div class="frameline">
              <span class="faint mono">{f.ts}</span>
              <span class="mono cmdcell">{f.cmd}</span>
              <span class="mono dim">{f.payload || "—"}</span>
            </div>
          {:else}
            <span class="faint">Aucune trame.</span>
          {/each}
        </div>
      </Card>

      <Card title="Journal">
        <div class="logs scroll">
          {#each logs.slice().reverse() as l}
            <div class="logline">
              <span class="faint mono">{l.ts}</span>
              <span>{l.line}</span>
            </div>
          {:else}
            <span class="faint">Aucune entrée.</span>
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
