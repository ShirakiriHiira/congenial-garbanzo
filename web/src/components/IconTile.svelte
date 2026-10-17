<script lang="ts">
  import CardSim from 'lucide-svelte/icons/card-sim';
  import Nfc from 'lucide-svelte/icons/nfc';
  import type { CardType } from '../lib/api/types';

  let { cardType }: { cardType: CardType } = $props();
</script>

<span class="icon-tile" class:esim={cardType === 'esim'}>
  {#if cardType === 'physical_sim'}
    <CardSim size={21} strokeWidth={1.7} aria-hidden="true" />
  {:else}
    <Nfc size={22} strokeWidth={1.7} aria-hidden="true" />
  {/if}
</span>

<style>
  .icon-tile{display:inline-flex;align-items:center;justify-content:center;width:34px;height:34px;flex:0 0 34px;border-radius:8px;background:var(--icon-physical-bg);color:var(--icon-physical-fg)}
  .esim{background:var(--icon-esim-bg);color:var(--icon-esim-fg)}
</style>
