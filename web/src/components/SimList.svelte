<script lang="ts">
  import type { OverviewItem } from '../lib/api/types';
  import SimRow from './SimRow.svelte';

  let { items, onopen }: { items: OverviewItem[]; onopen: (id: string) => void } = $props();
</script>

<ul class="sim-list">
  {#each items as item (item.sim.id)}
    <li><SimRow sim={item.sim} period={item.primary_period} {onopen} /></li>
  {/each}
</ul>

<style>
  .sim-list{list-style:none;margin:0;padding:0;min-width:0}
  li{margin:0;padding:0;min-width:0}
</style>
