<script lang="ts">
  let { message, onretry }: { message: string; onretry?: () => void } = $props();
</script>

<div class="error notice" role="alert">
  <p>{message}</p>
  {#if onretry}<button type="button" onclick={onretry}>重试</button>{/if}
</div>

<style>p{margin:0;overflow-wrap:anywhere}</style>
