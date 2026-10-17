<script lang="ts">
  import { labels } from '../lib/domain/labels';

  let { label, onadd, disabled = false }: {
    label: string;
    onadd: () => void;
    disabled?: boolean;
  } = $props();
</script>

<footer>
  <span class="explanation">{label}</span>
  <button type="button" onclick={onadd} {disabled}>
    <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" aria-hidden="true"><path d="M10 3v14M3 10h14" /></svg>
    <span>{labels.addCard}</span>
  </button>
</footer>

<style>
  footer{position:sticky;bottom:0;z-index:1;display:flex;align-items:center;justify-content:space-between;gap:12px;margin-top:auto;padding:10px 14px calc(10px + env(safe-area-inset-bottom));border-top:1px solid var(--border);background:var(--page-bg)}
  .explanation{min-width:0;font-size:11px;line-height:1.5;color:var(--text-secondary);overflow-wrap:anywhere}
  button{display:flex;align-items:center;justify-content:center;gap:6px;flex-shrink:0;min-height:44px;border:1px solid var(--header-bg);border-radius:999px;padding:9px 14px;background:var(--header-bg);color:var(--text-on-dark);font-size:12px;font-weight:600}
</style>
