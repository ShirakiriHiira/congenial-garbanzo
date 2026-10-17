<script lang="ts">
  import type { Snippet } from 'svelte';
  import { labels } from '../lib/domain/labels';
  import AvatarButton from './AvatarButton.svelte';

  let { firstName, photoUrl, onsettings, children }: {
    firstName: string;
    photoUrl: string | null;
    onsettings: () => void;
    children: Snippet;
  } = $props();
</script>

<header>
  <div class="heading">
    <div class="brand">
      <p>{labels.appName}</p>
      <h1>{labels.title}</h1>
    </div>
    <AvatarButton {firstName} {photoUrl} onclick={onsettings} />
  </div>
  {@render children()}
</header>

<style>
  header{display:grid;gap:12px;min-width:0;min-height:120px;padding:14px;background:var(--header-bg);color:var(--text-on-dark)}
  .heading{display:flex;align-items:center;justify-content:space-between;gap:12px;min-width:0}
  .brand{min-width:0;overflow-wrap:anywhere}
  p{margin:0 0 4px;color:var(--text-muted);font-size:10px;font-weight:600;line-height:1.4;letter-spacing:.08em;text-transform:uppercase}
  h1{margin:0;font-size:16px;font-weight:700;line-height:1.4;color:var(--text-on-dark)}
</style>
