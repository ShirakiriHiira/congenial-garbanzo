<script lang="ts">
  let { message = '', ondismiss }: { message?: string; ondismiss?: () => void } = $props();
</script>

<div role="status" aria-live="polite" aria-atomic="true">
  {#if message}
    <div class="notice toast"><span>{message}</span>{#if ondismiss}<button type="button" aria-label="关闭提示" onclick={ondismiss}>关闭</button>{/if}</div>
  {/if}
</div>

<style>.toast{display:flex;align-items:center;justify-content:space-between;gap:8px}.toast span{overflow-wrap:anywhere}</style>
