<script>
  import { t } from "./i18n.js";

  let { state } = $props();

  // Battery readings, paired with their label key so the status bar and the
  // low-battery banner name the same thing the same way.
  const batteries = $derived([
    { key: "battery.left", value: state.batteryLeft },
    { key: "battery.right", value: state.batteryRight },
    { key: "battery.case", value: state.batteryCase },
  ]);

  function battClass(v) {
    if (v <= state.batteryThresh) return "bad";
    if (v <= state.batteryThresh + 15) return "warn";
    return "good";
  }
</script>

<div class="statusbar">
  <div class="batts">
    {#each batteries as b}
      <div class="batt">
        <span class="faint">{$t(b.key)}</span>
        <div class="bar">
          <div class="fill {battClass(b.value)}" style="width:{b.value}%"></div>
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

<style>
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
</style>
