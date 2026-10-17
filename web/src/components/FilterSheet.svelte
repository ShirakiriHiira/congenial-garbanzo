<script module lang="ts">
  export type FilterValue = 'all' | 'due_soon' | 'expired' | 'archived';
</script>

<script lang="ts">
  import { onDestroy } from 'svelte';
  import { labels } from '../lib/domain/labels';

  let { open = $bindable(false), value, loadedCount, total, hasMore, onselect }: {
    open: boolean;
    value: FilterValue;
    loadedCount: number;
    total: number;
    hasMore: boolean;
    onselect: (value: FilterValue) => void;
  } = $props();
  const uid = $props.id();
  const options: { value: FilterValue; label: string }[] = [
    { value: 'all', label: labels.filterAll },
    { value: 'due_soon', label: labels.dueSoon },
    { value: 'expired', label: labels.expiredCount },
    { value: 'archived', label: labels.filterArchived },
  ];
  let dialog = $state<HTMLDialogElement>();
  let previousFocus: HTMLElement | null = null;

  function restoreFocus() {
    if (previousFocus?.isConnected) previousFocus.focus();
    previousFocus = null;
  }
  function closeDialog() {
    if (typeof dialog?.close === 'function' && dialog.open) dialog.close();
  }
  function close() {
    open = false;
    closeDialog();
    restoreFocus();
  }
  function select(next: FilterValue) {
    onselect(next);
    close();
  }
  $effect(() => {
    if (open && dialog && !dialog.open) {
      previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      dialog.showModal();
      dialog.querySelector<HTMLInputElement>('input:checked')?.focus();
    } else if (!open && dialog?.open) {
      dialog.close();
      restoreFocus();
    }
  });
  $effect(() => {
    const element = dialog;
    if (!element) return;
    function onBackdropClick(event: MouseEvent) {
      if (event.target !== element) return;
      const bounds = element.getBoundingClientRect();
      if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) close();
    }
    element.addEventListener('click', onBackdropClick);
    return () => element.removeEventListener('click', onBackdropClick);
  });
  onDestroy(() => {
    closeDialog();
    restoreFocus();
  });
</script>

<dialog bind:this={dialog} aria-labelledby={`${uid}-title`} oncancel={(event) => { event.preventDefault(); close(); }} onclose={() => { open = false; restoreFocus(); }}>
  <div class="sheet-header">
    <h2 id={`${uid}-title`}>{labels.filter}</h2>
    <button type="button" class="close" aria-label={labels.close} onclick={close}>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" /></svg>
    </button>
  </div>
  <fieldset>
    <legend class="sr-only">{labels.filter}</legend>
    {#each options as option}
      <label>
        <input type="radio" name={`${uid}-filter`} value={option.value} checked={value === option.value} onchange={() => select(option.value)} />
        <span>{option.label}</span>
      </label>
    {/each}
  </fieldset>
  {#if open}
    <p class="count">{labels.loadedOfTotal(loadedCount, total)}</p>
    {#if hasMore && (value === 'due_soon' || value === 'expired')}
      <p class="note">{labels.filterLoadedOnly}</p>
    {/if}
    {#if value === 'archived'}
      <p class="note">{labels.filterArchivedSource}</p>
    {/if}
  {/if}
</dialog>

<style>
  dialog{position:fixed;inset:auto 0 0;width:calc(100% - 32px);max-width:448px;max-height:calc(100dvh - 32px);margin:0 auto;padding:14px 16px calc(16px + env(safe-area-inset-bottom));border:1px solid var(--border-strong);border-radius:12px 12px 0 0;background:var(--page-bg);color:var(--text);overflow:auto}
  dialog::backdrop{background:var(--header-bg);opacity:.7}
  .sheet-header{display:flex;justify-content:space-between;align-items:center;gap:12px}
  h2{font-size:16px;margin:0;overflow-wrap:anywhere}
  .close{display:flex;align-items:center;justify-content:center;width:44px;height:44px;padding:0;border:0;border-radius:999px;background:transparent;color:var(--text-secondary)}
  fieldset{padding:0;margin:4px 0 12px;border:0;border-radius:0}
  label{display:flex;align-items:center;gap:12px;min-height:44px;margin:0;padding:9px 0;border-bottom:1px solid var(--border);font-size:13px;font-weight:400;cursor:pointer}
  label span{overflow-wrap:anywhere}
  input{flex:0 0 20px;width:20px;height:20px;min-height:20px;margin:0;padding:0;accent-color:var(--icon-esim-fg)}
  .count,.note{margin:8px 0 0;font-size:12px;line-height:1.5;color:var(--text-secondary);overflow-wrap:anywhere}
</style>
