<script>
  import { t } from "./i18n.js";

  export let title = "";
  export let hint = "";
  /** Feature absent from the model: the card is dimmed and explained. */
  export let unsupported = false;
  /** Behaviour inferred from analysis but never validated on the wire. */
  export let unverified = false;
</script>

<section class="card" class:unsupported>
  <header>
    <h3>{title}</h3>
    {#if unverified}
      <span class="badge" title={$t("badge.unverified.title")}>
        {$t("badge.unverified")}
      </span>
    {/if}
    {#if unsupported}
      <span class="badge muted">{$t("badge.unsupported")}</span>
    {/if}
  </header>
  {#if hint}<p class="hint">{hint}</p>{/if}
  <div class="body" class:disabled={unsupported}>
    <slot />
  </div>
</section>

<style>
  .card {
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 16px 18px;
  }
  header { display: flex; align-items: center; gap: 9px; margin-bottom: 2px; }
  h3 { margin: 0; font-size: 0.98rem; font-weight: 700; letter-spacing: 0.2px; }
  .hint {
    margin: 6px 0 0;
    font-size: 0.82rem;
    color: var(--text-dim);
    line-height: 1.45;
  }
  .body { margin-top: 14px; }
  .body.disabled { opacity: 0.4; pointer-events: none; }
  .badge {
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    font-weight: 700;
    padding: 2px 7px;
    border-radius: 20px;
    background: #3a2e12;
    color: var(--warn);
    border: 1px solid #5c4818;
  }
  .badge.muted {
    background: var(--bg-input);
    color: var(--text-faint);
    border-color: var(--border-strong);
  }
</style>
