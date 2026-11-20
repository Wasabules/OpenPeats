<script>
  // Desktop integration on Linux: the launcher's actions and the GNOME Quick
  // Settings toggle, installed for this user by the application itself. Go does
  // the work and reports where each piece stands; this card only shows it and
  // offers what can be done next. Elsewhere Go reports nothing available and
  // the card stays hidden.
  import { onMount } from "svelte";
  import Card from "./Card.svelte";
  import { t } from "./i18n.js";
  import {
    DesktopIntegration, InstallDesktopIntegration, RemoveDesktopIntegration,
  } from "../../wailsjs/go/main/App";

  let status = $state(/** @type {import("../../wailsjs/go/models").main.Integration | null} */ (null));
  let working = $state(false);

  onMount(async () => {
    try {
      status = await DesktopIntegration();
    } catch {
      status = null; // bindings not ready: the card stays hidden
    }
  });

  async function act(fn) {
    working = true;
    try {
      status = await fn();
    } finally {
      working = false;
    }
  }

  const PIECE = {
    missing: "integration.state.missing",
    installed: "integration.state.installed",
    outdated: "integration.state.outdated",
    system: "integration.state.system",
  };

  const needs = (s) => s === "missing" || s === "outdated";
  const ours = (s) => s === "installed" || s === "outdated";
  const present = (s) => s === "installed" || s === "system";

  // The extension's line says whether it runs, not only whether its files are
  // there: installed and switched on, it still waits for the next login.
  const extensionKey = $derived.by(() => {
    if (!status || needs(status.extension)) return PIECE[status?.extension] ?? PIECE.missing;
    if (status.extensionActive) return "integration.extension.active";
    if (status.extensionEnabled) return "integration.extension.nextLogin";
    return "integration.extension.off";
  });

  const canInstall = $derived(!!status && (
    needs(status.launcher) ||
    (status.gnome && (needs(status.extension) || !status.extensionEnabled))
  ));
  const canRemove = $derived(!!status && (
    ours(status.launcher) || ours(status.extension) ||
    (status.gnome && status.extensionEnabled)
  ));
  const outdated = $derived(
    !!status && (status.launcher === "outdated" || status.extension === "outdated")
  );
</script>

{#if status?.available}
  <Card title={$t("card.integration.title")} hint={$t("card.integration.hint")}>
    <ul class="pieces">
      <li>
        <span>{$t("integration.launcher")}</span>
        <span class="tag" class:on={present(status.launcher)}>
          {$t(PIECE[status.launcher] ?? PIECE.missing)}
        </span>
      </li>
      {#if status.gnome}
        <li>
          <span>{$t("integration.extension")}</span>
          <span class="tag" class:on={status.extensionActive}>{$t(extensionKey)}</span>
        </li>
      {/if}
    </ul>

    {#if status.gnome && status.extensionEnabled && !status.extensionActive}
      <p class="note">{$t("integration.nextLogin")}</p>
    {/if}
    {#if status.gnome && status.userExtensionsDisabled}
      <p class="note warn">{$t("integration.userExtensionsDisabled")}</p>
    {/if}
    {#if status.error?.key}
      <p class="note warn">{$t(status.error)}</p>
    {/if}

    {#if canInstall || canRemove}
      <div class="actions">
        {#if canRemove}
          <button onclick={() => act(RemoveDesktopIntegration)} disabled={working}>
            {$t("action.removeIntegration")}
          </button>
        {/if}
        {#if canInstall}
          <button class="primary" onclick={() => act(InstallDesktopIntegration)} disabled={working}>
            {working ? $t("action.working")
              : outdated ? $t("action.updateIntegration") : $t("action.installIntegration")}
          </button>
        {/if}
      </div>
    {/if}
  </Card>
{/if}

<style>
  .pieces {
    list-style: none; margin: 10px 0 0; padding: 0;
    display: flex; flex-direction: column; gap: 8px; font-size: 0.88rem;
  }
  .pieces li { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .tag {
    font-size: 0.74rem; padding: 2px 9px; border-radius: 20px;
    border: 1px solid var(--border-strong); color: var(--text-faint);
    white-space: nowrap;
  }
  .tag.on { color: var(--good); border-color: rgba(63, 191, 127, 0.45); }
  .note { margin: 12px 0 0; font-size: 0.82rem; line-height: 1.5; color: var(--text-dim); }
  .note.warn { color: #f6cf9a; }
  .actions { margin-top: 14px; display: flex; justify-content: flex-end; gap: 8px; }
</style>
