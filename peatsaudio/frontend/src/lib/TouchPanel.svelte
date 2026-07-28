<script>
  import { createEventDispatcher } from "svelte";
  const dispatch = createEventDispatcher();

  export let mappings = [];
  export let busy = false;

  const GESTURES = [
    { code: 0x01, label: "Clic — gauche" },
    { code: 0x02, label: "Clic — droit" },
    { code: 0x03, label: "Double clic — gauche" },
    { code: 0x04, label: "Double clic — droit" },
    { code: 0x05, label: "Triple clic — gauche" },
    { code: 0x06, label: "Triple clic — droit" },
    { code: 0x07, label: "Appui long — gauche" },
    { code: 0x08, label: "Appui long — droit" },
  ];

  const ACTIONS = [
    { code: 0x01, label: "Volume +" },
    { code: 0x02, label: "Volume −" },
    { code: 0x03, label: "Lecture / pause" },
    { code: 0x04, label: "Mode jeu" },
    { code: 0x05, label: "Piste précédente" },
    { code: 0x06, label: "Piste suivante" },
    { code: 0x07, label: "Mode ANC" },
    { code: 0x08, label: "Assistant vocal" },
  ];

  // Table de travail : le mapping lu sur les écouteurs, complété par les
  // gestes absents de la réponse.
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
        <span class="gesture">{g.label}</span>
        <select
          value={(draft.find((d) => d.gesture === g.code) || {}).action || 0}
          on:change={(e) => setAction(g.code, e.currentTarget.value)}
          disabled={busy}
        >
          <option value={0}>— non assigné —</option>
          {#each ACTIONS as a}
            <option value={a.code}>{a.label}</option>
          {/each}
        </select>
      </div>
    {/each}
  </div>

  <div class="warn-box">
    La lecture du mapping est fiable, mais son <strong>écriture</strong> n'a
    jamais pu être validée sur ce modèle. Vérifiez le résultat après application.
  </div>

  <div class="actions">
    <button class="primary" disabled={busy}
            on:click={() => dispatch("apply", draft.filter((d) => d.action !== 0))}>
      Appliquer le mapping
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
