<script lang="ts">
  import CardSim from 'lucide-svelte/icons/card-sim';
  import Nfc from 'lucide-svelte/icons/nfc';
  import Server from 'lucide-svelte/icons/server';
  import CreditCard from 'lucide-svelte/icons/credit-card';
  import Globe from 'lucide-svelte/icons/globe';
  import Layers from 'lucide-svelte/icons/layers';
  import type { CardType, Category } from '../lib/api/types';

  let { category = undefined, cardType = undefined }: {
    category?: Category;
    cardType?: CardType | null;
  } = $props();
  const isSim = $derived(cardType !== undefined ? cardType !== null : category?.kind === 'sim');
  const code = $derived(isSim ? 'sim' : category?.code ?? 'other');
  const esim = $derived(isSim && cardType === 'esim');
</script>

<span class="icon-tile" class:sim={isSim} class:esim>
  {#if isSim}
    {#if esim}<Nfc size={22} strokeWidth={1.7} aria-hidden="true" />{:else}<CardSim size={21} strokeWidth={1.7} aria-hidden="true" />{/if}
  {:else if code === 'vps'}
    <Server size={21} strokeWidth={1.7} aria-hidden="true" />
  {:else if code === 'subscription'}
    <CreditCard size={21} strokeWidth={1.7} aria-hidden="true" />
  {:else if code === 'domain'}
    <Globe size={21} strokeWidth={1.7} aria-hidden="true" />
  {:else}
    <Layers size={21} strokeWidth={1.7} aria-hidden="true" />
  {/if}
</span>

<style>
  .icon-tile{display:inline-flex;align-items:center;justify-content:center;width:34px;height:34px;flex:0 0 34px;border-radius:8px;background:var(--icon-generic-bg,var(--icon-physical-bg));color:var(--icon-generic-fg,var(--icon-physical-fg))}
  .sim{background:var(--icon-physical-bg);color:var(--icon-physical-fg)}
  .esim{background:var(--icon-esim-bg);color:var(--icon-esim-fg)}
</style>
