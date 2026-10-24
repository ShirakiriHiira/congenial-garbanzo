<script lang="ts">
  import { isValidTimeZone, timeZoneOptions } from '../lib/domain/datetime';
  import FormField from './FormField.svelte';
  let { id, value = $bindable(''), disabled = false, error }: {
    id: string; value?: string; disabled?: boolean; error?: string;
  } = $props();
  const zones = timeZoneOptions();
  const validation = $derived(error || (value && !isValidTimeZone(value) ? '请输入有效的 IANA 时区，不能使用 Local' : ''));
  let control = $state<HTMLInputElement | HTMLSelectElement>();
  $effect(() => { control?.setCustomValidity(validation); });
</script>

<FormField {id} label="时区" required error={validation}>
  {#snippet children(field)}
    {#if zones.length}
      <select bind:this={control} {id} bind:value required {disabled} aria-describedby={field.describedby} aria-invalid={field.invalid}>
        <option value="" disabled>请选择时区</option>
        {#if value && !zones.includes(value)}<option value={value}>{value}</option>{/if}
        {#each zones as zone}<option value={zone}>{zone}</option>{/each}
      </select>
    {:else}
      <input bind:this={control} {id} type="text" bind:value required {disabled} spellcheck={false} autocapitalize="off" aria-describedby={field.describedby} aria-invalid={field.invalid} />
    {/if}
  {/snippet}
</FormField>
