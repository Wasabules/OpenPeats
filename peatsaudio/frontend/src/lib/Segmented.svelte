<script>
  import { createEventDispatcher } from "svelte";
  const dispatch = createEventDispatcher();

  /** [{ value, label, note? }] */
  export let options = [];
  export let value = null;
  export let disabled = false;
</script>

<div class="seg" class:disabled>
  {#each options as o (o.value)}
    <button
      type="button"
      class:active={o.value === value}
      {disabled}
      on:click={() => dispatch("change", o.value)}
      title={o.note || ""}
    >
      {o.label}
    </button>
  {/each}
</div>

<style>
  .seg {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .seg.disabled { opacity: 0.45; pointer-events: none; }
  button {
    flex: 1 1 auto;
    min-width: 96px;
    padding: 8px 12px;
    font-size: 0.87rem;
    background: var(--bg-input);
  }
  button.active {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
    font-weight: 600;
  }
</style>
