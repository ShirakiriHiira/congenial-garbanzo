<script lang="ts">
  import type { CardType } from '../lib/api/types';

  let { cardType }: { cardType: CardType } = $props();
</script>

<span class="icon-tile" class:esim={cardType === 'esim'}>
  {#if cardType === 'physical_sim'}
    <svg width="21" height="21" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <rect x="3" y="16" width="3" height="5" rx="1" />
      <rect x="8" y="12" width="3" height="9" rx="1" />
      <rect x="13" y="7" width="3" height="14" rx="1" />
      <rect x="18" y="3" width="3" height="18" rx="1" />
    </svg>
  {:else}
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <ellipse cx="12" cy="12" rx="4" ry="9" />
      <path d="M3 12h18M5 6.5h14M5 17.5h14" />
    </svg>
  {/if}
</span>

<style>
  .icon-tile{display:inline-flex;align-items:center;justify-content:center;width:34px;height:34px;flex:0 0 34px;border-radius:8px;background:var(--icon-physical-bg);color:var(--icon-physical-fg)}
  .esim{background:var(--icon-esim-bg);color:var(--icon-esim-fg)}
</style>
