<script lang="ts">
  import { wallTimeCandidates, type Occurrence } from '../lib/domain/datetime';
  import FormField from './FormField.svelte';
  let { id, value = $bindable(''), occurrence = $bindable<Occurrence | undefined>(), zone, label = '日期和时间', required = false, disabled = false, error }: {
    id: string; value?: string; occurrence?: Occurrence; zone: string; label?: string;
    required?: boolean; disabled?: boolean; error?: string;
  } = $props();
  const resolution = $derived.by(() => {
    if (!value) return { candidates: [], error: '' };
    try { return { candidates: wallTimeCandidates(value, zone), error: '' }; }
    catch (cause) { return { candidates: [], error: cause instanceof Error ? cause.message : '日期和时间无效' }; }
  });
  const validation = $derived(error || resolution.error || (resolution.candidates.length > 1 && !occurrence ? '该时间出现两次，请选择明确的时区偏移' : ''));
  let input = $state<HTMLInputElement>();
  $effect(() => { input?.setCustomValidity(validation); });
</script>

<FormField {id} {label} {required} error={validation} hint={`使用时区：${zone}`}>
  {#snippet children(field)}
    <input bind:this={input} {id} type="datetime-local" step="1" bind:value oninput={() => { occurrence = undefined; }} {required} {disabled} aria-describedby={field.describedby} aria-invalid={field.invalid} />
  {/snippet}
</FormField>
{#if resolution.candidates.length > 1}
  <fieldset {disabled} aria-describedby={validation ? `${id}-error` : undefined}>
    <legend>选择本次时间的时区偏移</legend>
    {#each resolution.candidates as candidate}
      <label class="choice"><input type="radio" name={`${id}-occurrence`} value={candidate.occurrence} bind:group={occurrence} required />{candidate.occurrence === 'earlier' ? '第一次' : '第二次'}（UTC{candidate.offset}）</label>
    {/each}
  </fieldset>
{/if}

<style>
  fieldset{border:1px solid var(--border-strong);border-radius:8px;margin:8px 0 0;padding:8px;min-width:0}.choice{display:flex;align-items:center;gap:8px;min-height:44px}.choice input{width:auto;min-height:auto}
</style>
