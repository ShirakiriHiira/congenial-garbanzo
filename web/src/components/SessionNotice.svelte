<script lang="ts">
  import { labels } from '../lib/domain/labels';
  import { isTerminal, session } from '../lib/session.svelte';
  import ErrorBanner from './ErrorBanner.svelte';
</script>

{#if isTerminal()}
  <ErrorBanner message={session.auth === 'expired' ? labels.expired : labels.forbidden} />
{/if}
