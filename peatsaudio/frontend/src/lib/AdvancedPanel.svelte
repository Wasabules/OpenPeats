<script>
  import Card from "./Card.svelte";
  import { t } from "./i18n.js";

  let {
    busy = false,
    frames = [],
    logs = [],
    // The fields of the manual frame are bound from the parent, so they
    // survive switching tabs, which unmounts this panel.
    rawCmd = $bindable("0306"),
    rawPayload = $bindable(""),
    eqVendor = $bindable(false),
    /** Called with { cmd, payload, eqVendor } when the frame is sent. */
    onsend,
  } = $props();
</script>

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
      <button onclick={() => onsend?.({ cmd: rawCmd, payload: rawPayload, eqVendor })}
              disabled={busy}>{$t("action.send")}</button>
    </div>
    <label class="vendor">
      <input type="checkbox" bind:checked={eqVendor} />
      <span class="faint">{$t("field.rawEqVendor")}</span>
    </label>
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

<style>
  .single { display: flex; flex-direction: column; gap: 14px; max-width: 780px; }

  .raw { display: grid; grid-template-columns: 1fr 1fr auto; gap: 10px; align-items: end; }
  .raw label { display: flex; flex-direction: column; gap: 4px; font-size: 0.78rem; }
  .vendor {
    display: flex; align-items: center; gap: 7px;
    margin-top: 10px; font-size: 0.8rem;
  }

  .logs { height: 230px; font-size: 0.81rem; }
  .logline { display: flex; gap: 10px; padding: 2px 0; line-height: 1.45; }
  .frameline {
    display: grid; grid-template-columns: 92px 58px 1fr;
    gap: 10px; padding: 2px 0;
  }
  .cmdcell { color: var(--accent); font-weight: 700; }
</style>
