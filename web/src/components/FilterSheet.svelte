<script lang="ts">
  import { onDestroy } from 'svelte';
  import type { Category, OverviewCounts, ServiceStatusFilter } from '../lib/api/types';
  import { labels } from '../lib/domain/labels';

  let {
    open = $bindable(false), status, categoryID, categories, categoriesError, counts,
    categoryLoading, onstatus, oncategory, onreloadcategories,
  }: {
    open: boolean;
    status: ServiceStatusFilter;
    categoryID: string | null;
    categories: Category[];
    categoriesError: string;
    counts: OverviewCounts | null;
    categoryLoading: boolean;
    onstatus: (status: ServiceStatusFilter) => void;
    oncategory: (id: string) => void;
    onreloadcategories: () => void;
  } = $props();
  const uid = $props.id();
  const options: { value: ServiceStatusFilter; label: string }[] = [
    { value: 'active', label: labels.filterActive },
    { value: 'due_soon', label: labels.dueSoon },
    { value: 'expired', label: labels.expiredCount },
    { value: 'no_period', label: labels.filterNoPeriod },
    { value: 'archived', label: labels.filterArchived },
    { value: 'all', label: labels.filterAllWithArchived },
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
  function select(next: ServiceStatusFilter) {
    onstatus(next);
    close();
  }
  function selectCategory(event: Event) {
    oncategory((event.currentTarget as HTMLSelectElement).value);
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
        <input type="radio" name={`${uid}-filter`} value={option.value} checked={status === option.value} onchange={() => select(option.value)} />
        <span>{option.label}</span>
      </label>
    {/each}
  </fieldset>
  <label class="category">
    <span>{labels.category}</span>
    <select value={categoryID ?? ''} onchange={selectCategory} disabled={categoryLoading}>
      <option value="">{labels.allCategories}</option>
      {#each categories as category}
        <option value={category.id}>{category.name}</option>
      {/each}
    </select>
  </label>
  {#if categoriesError}
    <p class="note" role="alert">{categoriesError} <button type="button" class="text-button" onclick={onreloadcategories}>{labels.retry}</button></p>
  {/if}
  {#if open && counts}
    <p class="count">{labels.cardsTotal(counts.total)}</p>
  {/if}
  {#if status === 'archived'}
    <p class="note">{labels.filterArchivedNote}</p>
  {/if}
</dialog>

<style>
  dialog{position:fixed;inset:auto 0 0;width:calc(100% - 32px);max-width:448px;max-height:calc(100dvh - 32px);margin:0 auto;padding:14px 16px calc(16px + env(safe-area-inset-bottom));border:1px solid var(--border-strong);border-radius:12px 12px 0 0;background:var(--page-bg);color:var(--text);overflow:auto}
  dialog::backdrop{background:rgba(0,0,0,.55)}
  .sheet-header{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:8px}
  h2{margin:0;font-size:14px;line-height:1.4}
  .close{display:inline-flex;align-items:center;justify-content:center;width:44px;height:44px;padding:0;border-color:transparent;background:transparent}
  fieldset{display:grid;gap:2px;margin:0;padding:0;border:0}
  label{display:flex;align-items:center;gap:10px;min-height:44px;font-size:13px;color:var(--text)}
  input{width:18px;height:18px;margin:0;accent-color:var(--icon-esim-fg)}
  .category{display:grid;gap:6px;margin-top:8px}
  .category select{width:100%;min-height:44px;padding:8px 10px;border:1px solid var(--border-strong);border-radius:10px;background:var(--page-bg);color:var(--text)}
  .count,.note{margin:8px 0 0;font-size:12px;line-height:1.5;color:var(--text-secondary);overflow-wrap:anywhere}
</style>
