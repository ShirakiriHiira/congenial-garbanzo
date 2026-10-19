<script lang="ts">
  import type { Category, ServiceOverviewItem } from '../lib/api/types';
  import ServiceRow from './ServiceRow.svelte';

  let { items, categories, onopen }: {
    items: ServiceOverviewItem[];
    categories: Category[];
    onopen: (id: string) => void;
  } = $props();
  const categoriesById = $derived(new Map(categories.map((category) => [category.id, category])));
</script>

<ul class="service-list">
  {#each items as item (item.service.id)}
    <li><ServiceRow service={item.service} periods={item.periods} category={categoriesById.get(item.service.category_id)} {onopen} /></li>
  {/each}
</ul>

<style>
  .service-list{list-style:none;margin:0;padding:0}
</style>
