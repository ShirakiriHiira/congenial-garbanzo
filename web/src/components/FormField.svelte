<script lang="ts">
  import type { Snippet } from 'svelte';
  let { id, label, hint, error, required = false, action, children }: {
    id: string; label: string; hint?: string; error?: string; required?: boolean;
    action?: Snippet;
    children: Snippet<[{ id: string; describedby: string | undefined; invalid: boolean }]>;
  } = $props();
  const describedby = $derived([hint ? `${id}-hint` : '', error ? `${id}-error` : ''].filter(Boolean).join(' ') || undefined);
</script>

<div class="field">
  <div class="label-row">
    <label for={id}>{label}{#if required}<span aria-hidden="true"> *</span>{/if}</label>
    {#if action}{@render action()}{/if}
  </div>
  {@render children({ id, describedby, invalid: Boolean(error) })}
  {#if hint}<p id={`${id}-hint`} class="hint">{hint}</p>{/if}
  {#if error}<p id={`${id}-error`} class="error" role="alert">{error}</p>{/if}
</div>

<style>
  .field{display:grid;gap:6px;min-width:0}.label-row{display:flex;align-items:center;justify-content:space-between;gap:8px;min-width:0}label{font-weight:600;margin:0;overflow-wrap:anywhere}p{margin:0}.hint{color:var(--text-secondary);font-size:12px}
</style>
