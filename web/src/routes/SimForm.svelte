<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api } from '../lib/api/resources';
  import { ApiError } from '../lib/api/errors';
  import type { CardType, Sim, SimCreateInput, SimUpdateInput } from '../lib/api/types';
  import { ConflictFlow } from '../lib/conflict.svelte';
  import { Submitter } from '../lib/form.svelte';
  import { acceptMe, canWrite, session } from '../lib/session.svelte';
  import { navigate } from '../lib/router';
  import { getInitData } from '../lib/telegram/sdk';
  import { cardTypeLabels, errorMessage, labels } from '../lib/domain/labels';
  import FormField from '../components/FormField.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let { id }: { id?: string } = $props();
  type OptionalKey = 'operator' | 'phone_number' | 'country_or_region' | 'iccid' | 'notes';
  const optionalFields: { key: OptionalKey; label: string; limit: number }[] = [
    { key: 'operator', label: '运营商', limit: 200 },
    { key: 'phone_number', label: '手机号', limit: 200 },
    { key: 'country_or_region', label: '国家或地区', limit: 200 },
    { key: 'iccid', label: 'ICCID', limit: 200 },
    { key: 'notes', label: '备注', limit: 4000 },
  ];
  let entity = $state<Sim | null>(null);
  let name = $state('');
  let cardType = $state<CardType>('physical_sim');
  let values = $state<Record<OptionalKey, string>>({ operator: '', phone_number: '', country_or_region: '', iccid: '', notes: '' });
  let cleared = $state<Partial<Record<OptionalKey, boolean>>>({});
  let loading = $state(false);
  let loadError = $state<unknown>(null);
  let errors = $state<Record<string, string>>({});
  const submitter = new Submitter();
  const controller = new AbortController();
  const conflict = new ConflictFlow<Sim>(() => api.sim(id ?? '', controller.signal));
  let alive = true;
  const dirty = $derived(name !== (entity?.name ?? '') || cardType !== (entity?.card_type ?? 'physical_sim') || optionalFields.some(({ key }) => values[key] !== (entity?.[key] ?? '') || cleared[key]));
  $effect(() => { session.dirty = dirty; });

  function fill(current: Sim) {
    entity = current;
    name = current.name;
    cardType = current.card_type;
    values = { operator: current.operator ?? '', phone_number: current.phone_number ?? '', country_or_region: current.country_or_region ?? '', iccid: current.iccid ?? '', notes: current.notes ?? '' };
    cleared = {};
  }
  async function load() {
    if (!getInitData() || session.auth === 'expired' || session.auth === 'forbidden') { loading = false; return; }
    loading = true;
    loadError = null;
    try {
      await Promise.all([
        id ? api.sim(id, controller.signal).then((current) => { if (alive) fill(current); }) : Promise.resolve(),
        session.me ? Promise.resolve() : api.me(controller.signal).then((user) => { if (alive) acceptMe(user); }),
      ]);
    } catch (cause) { if (alive && !controller.signal.aborted) loadError = cause; }
    finally { if (alive) loading = false; }
  }
  onMount(() => { if (id || !session.me) void load(); });
  onDestroy(() => { alive = false; controller.abort(); session.dirty = false; });

  function validate(): boolean {
    const next: Record<string, string> = {};
    if (!name.trim()) next.name = '请输入卡片名称';
    else if (Array.from(name).length > 200) next.name = '名称最多 200 个字符';
    for (const field of optionalFields) if (Array.from(values[field.key]).length > field.limit) next[field.key] = `最多 ${field.limit} 个字符`;
    errors = next;
    return Object.keys(next).length === 0;
  }
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!canWrite() || submitter.busy || loading || loadError || conflict.open || conflict.failed || !validate()) return;
    await submitter.run(async () => {
      const body: SimUpdateInput = {};
      if (!id || name !== entity?.name) body.name = name;
      if (!id || cardType !== entity?.card_type) body.card_type = cardType;
      for (const { key } of optionalFields) {
        if (id) {
          if (cleared[key]) { if (entity?.[key] !== null) body[key] = null; }
          else if (values[key] !== (entity?.[key] ?? '')) body[key] = values[key] === '' ? null : values[key];
        } else if (values[key] !== '') body[key] = values[key];
      }
      try {
        const saved = id ? await api.updateSim(id, body, controller.signal) : await api.createSim(body as SimCreateInput, controller.signal);
        if (!alive) return;
        fill(saved);
        session.dirty = false;
        navigate(`/app/sims/${saved.id}`);
      } catch (cause) {
        if (alive && id && cause instanceof ApiError && cause.kind === 'conflict') await conflict.present();
        throw cause;
      }
    });
  }
  function useLatest() {
    const current = conflict.current;
    const previous = entity;
    if (!current || conflict.loading || conflict.failed) return;
    entity = current;
    if (name === (previous?.name ?? '')) name = current.name;
    if (cardType === (previous?.card_type ?? 'physical_sim')) cardType = current.card_type;
    for (const { key } of optionalFields) {
      if (!cleared[key] && values[key] === (previous?.[key] ?? '')) values[key] = current[key] ?? '';
    }
    errors = {};
    submitter.clear();
    conflict.dismiss();
  }
</script>

<section class="content stack" aria-labelledby="sim-form-title">
  <h1 id="sim-form-title">{id ? '编辑卡片' : '添加卡片'}</h1>
  <p class="muted">卡片资料没有版本锁；多端同时修改时，以后提交的资料为准。</p>
  {#if loading}<p role="status">{labels.loading}</p>
  {:else if loadError}<ErrorBanner message={errorMessage(loadError)} onretry={session.auth === 'ready' ? load : undefined} />
  {:else}
    {#if !canWrite()}<p class="notice">{labels.readOnly}</p>{/if}
    <form class="stack" onsubmit={submit} novalidate>
      <FormField id="sim-name" label="卡片名称" required error={errors.name}>
        {#snippet children(field)}<input id={field.id} bind:value={name} disabled={!canWrite() || submitter.busy} required aria-invalid={field.invalid} aria-describedby={field.describedby} />{/snippet}
      </FormField>
      <FormField id="sim-card-type" label="卡片类型" required>
        {#snippet children(field)}<select id={field.id} bind:value={cardType} disabled={!canWrite() || submitter.busy}><option value="physical_sim">{cardTypeLabels.physical_sim}</option><option value="esim">{cardTypeLabels.esim}</option></select>{/snippet}
      </FormField>
      {#each optionalFields as field (field.key)}
        <FormField id={`sim-${field.key}`} label={field.label} error={errors[field.key]} hint={`最多 ${field.limit} 个字符`}>
          {#snippet children(input)}
            <div class="stack">
              {#if field.key === 'notes'}<textarea id={input.id} rows="4" bind:value={values[field.key]} oninput={() => { cleared[field.key] = false; }} disabled={!canWrite() || submitter.busy} aria-invalid={input.invalid} aria-describedby={input.describedby}></textarea>
              {:else}<input id={input.id} bind:value={values[field.key]} oninput={() => { cleared[field.key] = false; }} disabled={!canWrite() || submitter.busy} aria-invalid={input.invalid} aria-describedby={input.describedby} />{/if}
              <button type="button" class="clear" aria-label={`清除${field.label}`} disabled={!canWrite() || submitter.busy} onclick={() => { values[field.key] = ''; cleared[field.key] = Boolean(id); }}>清除</button>
            </div>
          {/snippet}
        </FormField>
      {/each}
      {#if submitter.error && !conflict.open}<ErrorBanner message={errorMessage(submitter.error)} />{/if}
      {#if conflict.failed && !conflict.open}<ErrorBanner message="无法读取最新资料，提交已暂停。" onretry={() => { void conflict.retry(); }} />{/if}
      <button type="submit" class="primary" disabled={!canWrite() || submitter.busy || conflict.open || conflict.failed || (Boolean(id) && !dirty)}>{submitter.busy ? '保存中…' : '保存卡片'}</button>
    </form>
  {/if}
</section>
<ConfirmDialog bind:open={conflict.open} title="卡片资料已变化" message="草稿已保留。取最新值将替换表单；请重新检查、编辑后再次保存，不会自动提交。" confirmLabel="取最新值继续编辑" busy={conflict.loading} onconfirm={useLatest} oncancel={() => conflict.dismiss()}>
  {#if conflict.loading}<p role="status">正在读取最新资料…</p>
  {:else if conflict.failed}<ErrorBanner message="最新资料读取失败，提交已暂停。" onretry={() => { void conflict.retry(); }} />
  {:else if conflict.current}<p>服务端名称：{conflict.current.name}</p><p>类型：{cardTypeLabels[conflict.current.card_type]}</p><p>最后更新：{conflict.current.updated_at}</p>{/if}
</ConfirmDialog>
<style>.clear{justify-self:start}.muted{color:var(--text-secondary);margin:0}</style>
