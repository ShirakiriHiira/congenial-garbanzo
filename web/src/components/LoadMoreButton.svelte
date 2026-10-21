<script lang="ts">
  import { labels } from '../lib/domain/labels';
  let { loading = false, disabled = false, onclick }: { loading?: boolean; disabled?: boolean; onclick: () => void } = $props();
</script>

<button type="button" {disabled} {onclick}>{loading ? labels.loading : labels.loadMore}</button>
