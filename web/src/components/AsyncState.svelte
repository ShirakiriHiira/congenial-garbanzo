<script lang="ts">
  import type { Snippet } from 'svelte';
  import ErrorBanner from './ErrorBanner.svelte';
  let { loading = false, error, empty = false, emptyMessage = '暂无数据', onretry, children }: {
    loading?: boolean; error?: string; empty?: boolean; emptyMessage?: string;
    onretry?: () => void; children?: Snippet;
  } = $props();
</script>

{#if loading}
  <p role="status" aria-live="polite">正在加载…</p>
{:else if error}
  <ErrorBanner message={error} {onretry} />
{:else if empty}
  <p class="notice">{emptyMessage}</p>
{:else if children}
  {@render children()}
{/if}
