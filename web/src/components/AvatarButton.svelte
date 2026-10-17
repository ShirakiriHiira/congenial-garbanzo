<script lang="ts">
  import { labels } from '../lib/domain/labels';

  let { firstName, photoUrl, onclick }: {
    firstName: string;
    photoUrl: string | null;
    onclick: () => void;
  } = $props();
  let failedUrl = $state<string | null>(null);
  const safePhotoUrl = $derived.by(() => {
    if (!photoUrl) return null;
    try {
      const url = new URL(photoUrl);
      return url.protocol === 'https:' ? url.href : null;
    } catch {
      return null;
    }
  });
  const initial = $derived(Array.from(firstName.trim())[0] ?? '');
  function onImageError(event: Event) {
    if (event.currentTarget instanceof HTMLImageElement) failedUrl = safePhotoUrl;
  }
</script>

<button type="button" aria-label={labels.settings} {onclick}>
  <span class="avatar">
    {#if safePhotoUrl && safePhotoUrl !== failedUrl}
      <img src={safePhotoUrl} alt={firstName} referrerpolicy="no-referrer" onerror={onImageError} />
    {:else if initial}
      <span class="initial" aria-hidden="true">{initial}</span>
    {:else}
      <svg width="21" height="21" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true">
        <circle cx="12" cy="8" r="3.5" /><path d="M5 21v-2a7 7 0 0 1 14 0v2" />
      </svg>
    {/if}
  </span>
</button>

<style>
  button{display:flex;align-items:center;justify-content:center;flex:0 0 44px;width:44px;height:44px;padding:0;border:0;background:transparent;border-radius:999px;color:var(--text-on-dark)}
  .avatar{display:flex;align-items:center;justify-content:center;width:33px;height:33px;overflow:hidden;border-radius:50%;background:var(--stat-bg)}
  img{display:block;width:100%;height:100%;object-fit:cover}
  .initial{font-size:14px;font-weight:600;line-height:1}
</style>
