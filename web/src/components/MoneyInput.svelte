<script lang="ts">
  import { parseMoney } from '../lib/domain/money';
  import FormField from './FormField.svelte';
  let { id, label = '金额', amount = $bindable(''), currency = $bindable(''), disabled = false, error }: {
    id: string; label?: string; amount?: string; currency?: string; disabled?: boolean; error?: string;
  } = $props();
  const validation = $derived.by(() => {
    if (error) return error;
    try { parseMoney(amount, currency); return ''; }
    catch (cause) { return cause instanceof Error ? cause.message : '金额无效'; }
  });
  let input = $state<HTMLInputElement>();
  $effect(() => { input?.setCustomValidity(validation); });
</script>

<div class="stack">
  <FormField {id} {label} error={validation}>
    {#snippet children(field)}
      <input bind:this={input} {id} type="text" inputmode="decimal" pattern="[0-9]*[.]?[0-9]*" bind:value={amount} {disabled} aria-describedby={field.describedby} aria-invalid={field.invalid} />
    {/snippet}
  </FormField>
  <FormField id={`${id}-currency`} label="币种">
    {#snippet children(field)}
      <input id={field.id} type="text" bind:value={currency} pattern={'[A-Z]{3}'} maxlength="3" autocapitalize="characters" spellcheck={false} {disabled} aria-invalid={Boolean(validation)} aria-describedby={field.describedby} />
    {/snippet}
  </FormField>
</div>
