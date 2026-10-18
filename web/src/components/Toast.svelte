<script lang="ts">
  let { message = '', ondismiss }: { message?: string; ondismiss?: () => void } = $props();
  const AUTO_DISMISS_MS = 3000;

  $effect(() => {
    if (!message || !ondismiss) return;
    const dismiss = ondismiss;
    const timer = setTimeout(dismiss, AUTO_DISMISS_MS);
    return () => clearTimeout(timer);
  });
</script>

<div class="toast-region" role="status" aria-live="polite" aria-atomic="true">
  {#if message}
    <div class="notice toast"><span>{message}</span>{#if ondismiss}<button type="button" aria-label="关闭提示" onclick={ondismiss}>关闭</button>{/if}</div>
  {/if}
</div>

<style>
  .toast-region{position:fixed;left:12px;right:12px;bottom:calc(12px + env(safe-area-inset-bottom));z-index:10;display:flex;justify-content:center;pointer-events:none}
  .toast{display:flex;align-items:center;justify-content:space-between;gap:8px;width:min(440px,100%);border:1px solid var(--border-strong);background:var(--page-bg);box-shadow:0 4px 16px rgb(0 0 0 / .18);pointer-events:auto}
  .toast span{overflow-wrap:anywhere}
</style>
