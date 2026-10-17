<script lang="ts">
  import { onDestroy, type Snippet } from 'svelte';
  let { open = $bindable(false), title, message, confirmLabel = '确认', cancelLabel = '取消', busy = false, onconfirm, oncancel, children }: {
    open?: boolean; title: string; message?: string; confirmLabel?: string; cancelLabel?: string;
    busy?: boolean; onconfirm: () => void; oncancel: () => void; children?: Snippet;
  } = $props();
  const uid = $props.id();
  let dialog = $state<HTMLDialogElement>();
  let previousFocus: HTMLElement | null = null;
  function restoreFocus() {
    if (previousFocus?.isConnected) previousFocus.focus();
    previousFocus = null;
  }
  function cancel() {
    if (busy) return;
    open = false;
    oncancel();
  }
  $effect(() => {
    if (open && dialog && !dialog.open) {
      previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      dialog.showModal();
    } else if (!open && dialog?.open) {
      dialog.close();
      restoreFocus();
    }
  });
  onDestroy(restoreFocus);
</script>

<dialog bind:this={dialog} aria-labelledby={`${uid}-title`} aria-describedby={message ? `${uid}-message` : undefined} oncancel={(event) => { event.preventDefault(); cancel(); }} onclose={() => { if (open) cancel(); restoreFocus(); }}>
  <div class="stack">
    <h2 id={`${uid}-title`}>{title}</h2>
    {#if message}<p id={`${uid}-message`}>{message}</p>{/if}
    {#if children}{@render children()}{/if}
    <div class="actions">
      <button type="button" onclick={cancel} disabled={busy}> {cancelLabel} </button>
      <button type="button" class="primary" onclick={onconfirm} disabled={busy}>{busy ? '处理中…' : confirmLabel}</button>
    </div>
  </div>
</dialog>

<style>
  dialog{width:calc(100% - 32px);max-width:448px;max-height:calc(100dvh - 32px);overflow:auto;padding:20px;border:1px solid var(--border-strong);border-radius:12px;background:var(--page-bg);color:var(--text)}dialog::backdrop{background:var(--header-bg);opacity:.7}h2{font-size:18px;margin:0}p{margin:0;overflow-wrap:anywhere}
</style>
