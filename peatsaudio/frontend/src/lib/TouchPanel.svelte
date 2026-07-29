<script>
  import { createEventDispatcher } from "svelte";
  import { t } from "./i18n.js";

  const dispatch = createEventDispatcher();

  export let mappings = [];
  export let busy = false;

  // Protocol codes paired with their catalogue key. The codes are wire
  // values and never change; only the label is translated.
  const GESTURES = [
    { code: 0x01, key: "touch.gesture.singleLeft" },
    { code: 0x02, key: "touch.gesture.singleRight" },
    { code: 0x03, key: "touch.gesture.doubleLeft" },
    { code: 0x04, key: "touch.gesture.doubleRight" },
    { code: 0x05, key: "touch.gesture.tripleLeft" },
    { code: 0x06, key: "touch.gesture.tripleRight" },
    { code: 0x07, key: "touch.gesture.longLeft" },
    { code: 0x08, key: "touch.gesture.longRight" },
  ];

  const ACTIONS = [
    { code: 0x01, key: "touch.action.volumeUp" },
    { code: 0x02, key: "touch.action.volumeDown" },
    { code: 0x03, key: "touch.action.playPause" },
    { code: 0x04, key: "touch.action.gameMode" },
    { code: 0x05, key: "touch.action.prevTrack" },
    { code: 0x06, key: "touch.action.nextTrack" },
    { code: 0x07, key: "touch.action.ancMode" },
    { code: 0x08, key: "touch.action.assistant" },
  ];

  // Working table: the mapping read from the earbuds, filled in with the
  // gestures the reply left out.
  let draft = [];
  $: draft = GESTURES.map((g) => {
    const found = mappings.find((m) => m.gesture === g.code);
    return { gesture: g.code, action: found ? found.action : 0 };
  });

  function setAction(gesture, action) {
    draft = draft.map((d) =>
      d.gesture === gesture ? { ...d, action: Number(action) } : d
    );
  }
</script>

<div class="wrap">
  <div class="map">
    {#each GESTURES as g}
      <div class="row">
        <span class="gesture">{$t(g.key)}</span>
        <select
          value={(draft.find((d) => d.gesture === g.code) || {}).action || 0}
          on:change={(e) => setAction(g.code, e.currentTarget.value)}
          disabled={busy}
        >
          <option value={0}>{$t("touch.unassigned")}</option>
          {#each ACTIONS as a}
            <option value={a.code}>{$t(a.key)}</option>
          {/each}
        </select>
      </div>
    {/each}
  </div>

  <!-- The warning carries inline emphasis, so it is a catalogue string with
       markup rather than a sentence assembled in the template. -->
  <div class="warn-box">{@html $t("touch.warning")}</div>

  <div class="actions">
    <button class="primary" disabled={busy}
            on:click={() => dispatch("apply", draft.filter((d) => d.action !== 0))}>
      {$t("action.applyMapping")}
    </button>
  </div>
</div>

<style>
  .wrap { display: flex; flex-direction: column; gap: 14px; }
  .map { display: grid; grid-template-columns: 1fr 1fr; gap: 9px 16px; }
  .row { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  .gesture { font-size: 0.87rem; }
  .row select { min-width: 165px; }
  .actions { display: flex; justify-content: flex-end; }

  @media (max-width: 780px) {
    .map { grid-template-columns: 1fr; }
  }
</style>
