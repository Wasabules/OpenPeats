<script>
  import { t } from "./i18n.js";

  let {
    busy = false,
    /** Called with { bands, masterGain } when the curve is sent. */
    onapply,
  } = $props();

  const FS = 48000;
  const F_MIN = 20;
  const F_MAX = 20000;
  const DB_RANGE = 12;
  const GAIN_MIN = -12;
  const GAIN_MAX = 12;
  const Q_MIN = 0.3;
  const Q_MAX = 6;

  // Granularity: normalised sets of centre frequencies.
  const GRANULARITY = {
    5: [60, 250, 1000, 4000, 12000],
    8: [40, 100, 250, 630, 1600, 4000, 10000, 16000],
    10: [31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000],
  };

  // A list rather than an object keyed by name: the display name is a
  // catalogue key now, so it can no longer double as the identifier.
  const PRESETS = [
    { key: "eq.preset.flat", apply: () => bands.map((b) => ({ ...b, gain: 0 })) },
    {
      key: "eq.preset.bass",
      apply: () =>
        bands.map((b) => ({ ...b, gain: b.freq <= 250 ? 6 : b.freq <= 500 ? 2 : 0 })),
    },
    {
      key: "eq.preset.vocal",
      apply: () =>
        bands.map((b) => ({
          ...b,
          gain: b.freq < 300 ? -3 : b.freq <= 4000 ? 4 : 0,
        })),
    },
    {
      key: "eq.preset.treble",
      apply: () =>
        bands.map((b) => ({ ...b, gain: b.freq >= 6000 ? 5 : b.freq >= 3000 ? 2 : 0 })),
    },
    {
      key: "eq.preset.loudness",
      apply: () =>
        bands.map((b) => ({
          ...b,
          gain:
            b.freq <= 120 ? 6 : b.freq >= 8000 ? 5 : b.freq >= 500 && b.freq <= 2000 ? -2 : 0,
        })),
    },
  ];

  // The curve belongs to this component: it is edited here and leaves only
  // through onapply, so it is internal state rather than a prop.
  let granularity = $state(8);
  let masterGain = $state(0);
  let bands = $state(GRANULARITY[8].map((f) => ({ freq: f, gain: 0, q: 1.4 })));

  function setGranularity(n) {
    granularity = n;
    // Keep the shape: the gain of each new band is interpolated from the
    // current curve.
    const old = bands;
    bands = GRANULARITY[n].map((f) => ({
      freq: f,
      gain: Math.max(GAIN_MIN, Math.min(GAIN_MAX, curveGainAt(f, old))),
      q: 1.4,
    }));
  }

  function applyPreset(preset) {
    bands = preset.apply();
    masterGain = 0;
  }

  function reset() {
    bands = bands.map((b) => ({ ...b, gain: 0 }));
    masterGain = 0;
  }

  // --- Response of an RBJ peaking filter, in dB ---
  function peakingDb(f, b) {
    if (!b.gain) return 0;
    const A = Math.pow(10, b.gain / 40);
    const w0 = (2 * Math.PI * b.freq) / FS;
    const alpha = Math.sin(w0) / (2 * Math.max(b.q, 0.05));

    const b0 = 1 + alpha * A, b1 = -2 * Math.cos(w0), b2 = 1 - alpha * A;
    const a0 = 1 + alpha / A, a1 = -2 * Math.cos(w0), a2 = 1 - alpha / A;

    const w = (2 * Math.PI * f) / FS;
    const cw = Math.cos(w), sw = Math.sin(w);
    const c2w = Math.cos(2 * w), s2w = Math.sin(2 * w);

    const nRe = b0 + b1 * cw + b2 * c2w, nIm = -(b1 * sw + b2 * s2w);
    const dRe = a0 + a1 * cw + a2 * c2w, dIm = -(a1 * sw + a2 * s2w);

    const den = Math.hypot(dRe, dIm);
    if (den === 0) return 0;
    return 20 * Math.log10(Math.hypot(nRe, nIm) / den);
  }

  const curveGainAt = (f, bs) => bs.reduce((acc, b) => acc + peakingDb(f, b), 0);

  // --- Plot geometry ---
  const W = 660, H = 210, PAD_L = 36, PAD_R = 10, PAD_T = 10, PAD_B = 24;
  const plotW = W - PAD_L - PAD_R;
  const plotH = H - PAD_T - PAD_B;

  const xOf = (f) =>
    PAD_L + ((Math.log10(f) - Math.log10(F_MIN)) /
      (Math.log10(F_MAX) - Math.log10(F_MIN))) * plotW;
  const fOf = (x) =>
    Math.pow(10, Math.log10(F_MIN) +
      ((x - PAD_L) / plotW) * (Math.log10(F_MAX) - Math.log10(F_MIN)));
  const yOf = (db) => PAD_T + plotH / 2 - (db / DB_RANGE) * (plotH / 2);
  const dbOf = (y) => ((PAD_T + plotH / 2 - y) / (plotH / 2)) * DB_RANGE;

  const curvePoints = $derived.by(() => {
    const pts = [];
    for (let i = 0; i <= 200; i++) {
      const f = F_MIN * Math.pow(F_MAX / F_MIN, i / 200);
      const db = Math.max(-DB_RANGE, Math.min(DB_RANGE, masterGain + curveGainAt(f, bands)));
      pts.push(`${xOf(f).toFixed(1)},${yOf(db).toFixed(1)}`);
    }
    return pts.join(" ");
  });

  // --- Direct manipulation of the points on the curve ---
  let svgEl = $state();
  let dragIndex = $state(-1);

  function svgPos(evt) {
    const r = svgEl.getBoundingClientRect();
    return {
      x: ((evt.clientX - r.left) / r.width) * W,
      y: ((evt.clientY - r.top) / r.height) * H,
    };
  }

  function onPointerDown(i, evt) {
    if (busy) return;
    dragIndex = i;
    evt.target.setPointerCapture(evt.pointerId);
  }

  function onPointerMove(evt) {
    if (dragIndex < 0) return;
    const { x, y } = svgPos(evt);
    const b = bands[dragIndex];

    let gain = Math.round(dbOf(y) * 2) / 2;
    gain = Math.max(GAIN_MIN, Math.min(GAIN_MAX, gain));

    // Horizontal movement is bounded by the neighbouring bands, to keep
    // frequencies in increasing order.
    let freq = Math.round(fOf(x));
    const lo = dragIndex > 0 ? bands[dragIndex - 1].freq * 1.05 : F_MIN;
    const hi = dragIndex < bands.length - 1 ? bands[dragIndex + 1].freq * 0.95 : F_MAX;
    freq = Math.max(lo, Math.min(hi, freq));

    bands[dragIndex] = { ...b, gain, freq };
  }

  function onPointerUp() { dragIndex = -1; }

  const GRID_DB = [-12, -6, 0, 6, 12];
  const GRID_F = [50, 100, 500, 1000, 5000, 10000];
  const fmtF = (f) => (f >= 1000 ? `${(f / 1000).toFixed(f % 1000 ? 1 : 0)}k` : `${f}`);
</script>

<div class="eq">
  <div class="toolbar">
    <div class="group">
      <span class="faint">{$t("eq.granularity")}</span>
      {#each Object.keys(GRANULARITY).map(Number) as n}
        <button class:active={granularity === n} disabled={busy}
                onclick={() => setGranularity(n)}>{$t("eq.bands", { count: n })}</button>
      {/each}
    </div>
    <div class="group">
      {#each PRESETS as preset}
        <button disabled={busy} onclick={() => applyPreset(preset)}>{$t(preset.key)}</button>
      {/each}
      <button disabled={busy} onclick={reset}>{$t("action.reset")}</button>
    </div>
  </div>

  <svg bind:this={svgEl} viewBox="0 0 {W} {H}" class="curve"
       onpointermove={onPointerMove}
       onpointerup={onPointerUp}
       onpointerleave={onPointerUp}
       role="application"
       aria-label={$t("eq.curveLabel")}>
    {#each GRID_DB as db}
      <line x1={PAD_L} x2={W - PAD_R} y1={yOf(db)} y2={yOf(db)}
            stroke={db === 0 ? "#3c4558" : "#212734"} stroke-width="1" />
      <text x={PAD_L - 7} y={yOf(db) + 4} class="axis end">{db > 0 ? "+" : ""}{db}</text>
    {/each}
    {#each GRID_F as f}
      <line x1={xOf(f)} x2={xOf(f)} y1={PAD_T} y2={H - PAD_B}
            stroke="#212734" stroke-width="1" />
      <text x={xOf(f)} y={H - 7} class="axis mid">{fmtF(f)}</text>
    {/each}

    <polyline points={curvePoints} fill="none" stroke="var(--accent)"
              stroke-width="2.5" stroke-linejoin="round" />

    {#each bands as b, i}
      <line x1={xOf(b.freq)} x2={xOf(b.freq)}
            y1={yOf(0)} y2={yOf(b.gain + masterGain)}
            stroke="#3d4558" stroke-width="1" />
      <!-- The handle really is a slider: dragging it sets the gain of the
           band. Kept out of the tab order because the fader below drives the
           same value and is the keyboard-accessible control. -->
      <circle class="handle" class:dragging={dragIndex === i}
              cx={xOf(b.freq)} cy={yOf(b.gain + masterGain)} r="7"
              role="slider" tabindex="-1"
              aria-label={$t("eq.gainAt", { freq: fmtF(b.freq) })}
              aria-valuemin={GAIN_MIN} aria-valuemax={GAIN_MAX}
              aria-valuenow={b.gain}
              onpointerdown={(e) => onPointerDown(i, e)} />
    {/each}
  </svg>

  <div class="faders" style="--n:{bands.length}">
    {#each bands as b, i}
      <div class="fader">
        <div class="q">
          <span class="faint">Q</span>
          <input type="number" min={Q_MIN} max={Q_MAX} step="0.1"
                 bind:value={b.q} disabled={busy}
                 title={$t("eq.qTitle")} />
        </div>

        <input class="slider" type="range" orient="vertical"
               min={GAIN_MIN} max={GAIN_MAX} step="0.5"
               bind:value={b.gain} disabled={busy}
               aria-label={$t("eq.gainAt", { freq: fmtF(b.freq) })} />

        <div class="val" class:zero={b.gain === 0}>
          {b.gain > 0 ? "+" : ""}{b.gain.toFixed(1)}
        </div>
        <div class="freq">{fmtF(b.freq)}</div>
      </div>
    {/each}
  </div>

  <div class="footer">
    <div class="master">
      <label for="mg">
        {$t("eq.masterGain")}
        <strong>{masterGain > 0 ? "+" : ""}{masterGain.toFixed(1)} dB</strong>
      </label>
      <input id="mg" type="range" min={GAIN_MIN} max={GAIN_MAX} step="0.5"
             bind:value={masterGain} disabled={busy} />
    </div>
    <button class="primary" disabled={busy}
            onclick={() => onapply?.({ bands: $state.snapshot(bands), masterGain })}>
      {busy ? $t("action.applying") : $t("action.apply")}
    </button>
  </div>

  <p class="faint note">{$t("eq.note")}</p>
</div>

<style>
  .eq { display: flex; flex-direction: column; gap: 14px; }

  .toolbar {
    display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap;
  }
  .group { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .group button { padding: 5px 11px; font-size: 0.82rem; }
  .group button.active {
    background: var(--accent); border-color: var(--accent);
    color: #fff; font-weight: 600;
  }

  .curve {
    width: 100%;
    background: #11141c;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    touch-action: none;
  }
  .axis { fill: var(--text-faint); font-size: 10px; }
  .axis.mid { text-anchor: middle; }
  .axis.end { text-anchor: end; }

  .handle {
    fill: var(--accent);
    stroke: #0e1016;
    stroke-width: 2;
    cursor: grab;
    transition: r 100ms;
  }
  .handle:hover { r: 9; }
  .handle.dragging { cursor: grabbing; fill: #8fb0ff; }

  .faders {
    display: grid;
    grid-template-columns: repeat(var(--n), 1fr);
    gap: 4px;
    padding: 12px 6px 6px;
    background: #11141c;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .fader { display: flex; flex-direction: column; align-items: center; gap: 7px; }

  .q { display: flex; flex-direction: column; align-items: center; gap: 2px; }
  .q span { font-size: 0.62rem; letter-spacing: 0.5px; }
  .q input {
    width: 46px; padding: 3px 4px;
    font-size: 0.72rem; text-align: center;
  }

  .slider {
    writing-mode: vertical-lr;
    direction: rtl;
    -webkit-appearance: slider-vertical;
    appearance: slider-vertical;
    width: 22px;
    height: 132px;
    padding: 0;
  }

  .val { font-size: 0.74rem; font-variant-numeric: tabular-nums; }
  .val.zero { color: var(--text-faint); }
  .freq { font-size: 0.7rem; color: var(--text-dim); }

  .footer { display: flex; align-items: flex-end; gap: 16px; }
  .master { flex: 1; display: flex; flex-direction: column; gap: 5px; }
  .master label { font-size: 0.84rem; color: var(--text-dim); }

  .note { font-size: 0.77rem; line-height: 1.5; margin: 0; }
</style>
