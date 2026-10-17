<script lang="ts">
  import type { Snippet } from 'svelte';

  let { title, children }: { title: string; children: Snippet } = $props();
</script>

<div class="section-header">
  <h2>{title}</h2>
  <div class="control">{@render children()}</div>
</div>

<style>
  .section-header{display:flex;align-items:center;justify-content:space-between;gap:10px;min-width:0}
  h2{margin:0;font-size:13px;font-weight:600;line-height:1.5;overflow-wrap:anywhere}
  .control{display:flex;align-items:center;flex-shrink:0}
</style>
