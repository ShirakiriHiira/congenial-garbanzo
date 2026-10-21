<script lang="ts">
  import type { Period } from '../lib/api/types';
  import { expiryDisplay } from '../lib/domain/expiry';
  import { labels } from '../lib/domain/labels';

  let { period }: { period: Period | null } = $props();
  const display = $derived(expiryDisplay(period));
</script>

{#if period}
  <span class="badge {display.tone}">{display.label}</span>
  {#if display.tone === 'ok' && display.days !== null}
    <span data-part="days">{labels.remainingDays(display.days)}</span>
  {/if}
{/if}

<style>
  .badge{display:inline-block;border-radius:999px;padding:3px 7px;font-size:10px;font-weight:600;line-height:1.3;overflow-wrap:anywhere}
  .muted{background:var(--border);color:var(--text-secondary)}
  .ok{background:var(--badge-ok-bg);color:var(--badge-ok-fg)}
  .warn{background:var(--badge-warn-bg);color:var(--badge-warn-fg)}
  .danger{background:var(--badge-danger-bg);color:var(--badge-danger-fg)}
  [data-part="days"]{color:var(--text-secondary);font-size:11px;overflow-wrap:anywhere}
</style>
