<script>
  import { createEventDispatcher } from "svelte";
  const dispatch = createEventDispatcher();

  export let label = "";
  export let checked = false;
  export let disabled = false;
  export let note = "";
</script>

<label class="row" class:disabled>
  <span class="text">
    <span class="label">{label}</span>
    {#if note}<span class="note">{note}</span>{/if}
  </span>
  <button
    type="button"
    class="switch"
    class:on={checked}
    {disabled}
    aria-pressed={checked}
    aria-label={label}
    on:click={() => dispatch("change", !checked)}
  >
    <span class="knob" />
  </button>
</label>

<style>
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 7px 0;
  }
  .row.disabled { opacity: 0.45; }
  .text { display: flex; flex-direction: column; gap: 2px; }
  .label { font-size: 0.92rem; }
  .note { font-size: 0.76rem; color: var(--text-faint); line-height: 1.35; }

  .switch {
    flex: none;
    width: 42px;
    height: 24px;
    padding: 0;
    border-radius: 20px;
    background: var(--bg-input);
    border: 1px solid var(--border-strong);
    position: relative;
    transition: background 140ms, border-color 140ms;
  }
  .switch.on { background: var(--accent); border-color: var(--accent); }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #cfd6e4;
    transition: transform 140ms;
  }
  .switch.on .knob { transform: translateX(18px); background: #fff; }
</style>
