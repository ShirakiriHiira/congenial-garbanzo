<script lang="ts">
  import type { DatePrecision } from '../lib/api/types';
  import type { Occurrence } from '../lib/domain/datetime';
  import DateTimeInput from './DateTimeInput.svelte';
  import FormField from './FormField.svelte';
  let { id, precision = $bindable<DatePrecision>('date'), expires = $bindable(''), occurrence = $bindable<Occurrence | undefined>(), zone, disabled = false, error }: {
    id: string; precision?: DatePrecision; expires?: string; occurrence?: Occurrence;
    zone: string; disabled?: boolean; error?: string;
  } = $props();
  function changePrecision(next: DatePrecision) {
    if (next === precision) return;
    precision = next;
    expires = '';
    occurrence = undefined;
  }
</script>

<div class="stack">
  <fieldset {disabled}>
    <legend>截止精度</legend>
    <label class="choice"><input type="radio" name={`${id}-precision`} value="date" checked={precision === 'date'} onchange={() => changePrecision('date')} />日期</label>
    <label class="choice"><input type="radio" name={`${id}-precision`} value="datetime" checked={precision === 'datetime'} onchange={() => changePrecision('datetime')} />精确时间</label>
  </fieldset>
  {#if precision === 'date'}
    <FormField {id} label="截止日期" required {error}>
      {#snippet children(field)}
        <input {id} type="date" bind:value={expires} required {disabled} aria-describedby={field.describedby} aria-invalid={field.invalid} />
      {/snippet}
    </FormField>
  {:else}
    <DateTimeInput {id} bind:value={expires} bind:occurrence {zone} {disabled} {error} required label="截止时间" />
  {/if}
</div>

<style>
  fieldset{border:0;padding:0;margin:0;min-width:0}.choice{display:flex;align-items:center;gap:8px;min-height:44px}.choice input{width:auto;min-height:auto}legend{font-weight:600;padding:0}
</style>
