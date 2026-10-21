<script lang="ts">
  import type { Snippet } from 'svelte';
  import { labels } from '../lib/domain/labels';
  import ErrorBanner from './ErrorBanner.svelte';
  let { loading = false, failed = false, failedMessage = '', onretry, children }: {
    loading?: boolean; failed?: boolean; failedMessage?: string;
    onretry?: () => void; children?: Snippet;
  } = $props();
</script>

{#if loading}<p role="status">{labels.loading}</p>
{:else if failed}<ErrorBanner message={failedMessage} {onretry} />
{:else}{@render children?.()}{/if}
