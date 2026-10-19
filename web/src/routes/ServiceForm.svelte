<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { api, fetchAllPages } from '../lib/api/resources';
  import { ApiError } from '../lib/api/errors';
  import type { Category, CategoryKind, CardType, ServiceCreateInput, ServiceItem, ServiceUpdateInput } from '../lib/api/types';
  import { ConflictFlow } from '../lib/conflict.svelte';
  import { Submitter } from '../lib/form.svelte';
  import { acceptMe, canWrite, onSessionFailure, session } from '../lib/session.svelte';
  import { navigate } from '../lib/router';
  import { getInitData } from '../lib/telegram/sdk';
  import { cardTypeLabels, errorMessage, fieldLabels, labels } from '../lib/domain/labels';
  import FormField from '../components/FormField.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let { id, initialKind }: { id?: string; initialKind?: CategoryKind } = $props();
  type GeneralKey = 'provider' | 'identifier' | 'management_url' | 'notes';
  type SimKey = 'phone_number' | 'country_or_region' | 'iccid';
  const generalFields: { key: GeneralKey; label: string; limit: number }[] = [
    { key: 'provider', label: '服务商', limit: 200 },
    { key: 'identifier', label: '公开标识', limit: 200 },
    { key: 'management_url', label: '管理链接', limit: 2048 },
    { key: 'notes', label: '备注', limit: 4000 },
  ];
  const simFields: { key: SimKey; label: string }[] = [
    { key: 'phone_number', label: '手机号' },
    { key: 'country_or_region', label: '国家或地区' },
    { key: 'iccid', label: 'ICCID' },
  ];
  let entity = $state<ServiceItem | null>(null);
  let categories = $state<Category[]>([]);
  let categoryID = $state('');
  let initialCategoryID = $state('');
  let name = $state('');
  let cardType = $state<CardType>('physical_sim');
  let general = $state<Record<GeneralKey, string>>({ provider: '', identifier: '', management_url: '', notes: '' });
  let sim = $state<Record<SimKey, string>>({ phone_number: '', country_or_region: '', iccid: '' });
  let cleared = $state<Record<string, boolean>>({});
  let loading = $state(false);
  let loadError = $state<unknown>(null);
  let errors = $state<Record<string, string>>({});
  let categoryBusy = $state(false);
  let categoryError = $state('');
  let categoryMode = $state<'closed' | 'create' | 'rename'>('closed');
  let categoryName = $state('');
  const submitter = new Submitter();
  const controller = new AbortController();
  const conflict = new ConflictFlow<ServiceItem>(() => api.service(id ?? '', controller.signal));
  let alive = true;
  const category = $derived(categories.find((item) => item.id === categoryID) ?? null);
  const serviceFields = $derived(fieldLabels(category?.code));
  const kind = $derived(category?.kind ?? initialKind ?? 'generic');
  const entityKind = $derived(entity?.sim_details ? 'sim' : entity ? categories.find((item) => item.id === entity?.category_id)?.kind ?? 'generic' : null);
  const dirty = $derived(entity
    ? name !== entity.name || categoryID !== entity.category_id || generalFields.some(({ key }) => general[key] !== (entity?.[key] ?? '') || cleared[key]) || (kind === 'sim' && (cardType !== (entity.sim_details?.card_type ?? 'physical_sim') || simFields.some(({ key }) => sim[key] !== (entity?.sim_details?.[key] ?? '') || cleared[key])))
    : name !== '' || categoryID !== initialCategoryID || generalFields.some(({ key }) => general[key] !== '') || cardType !== 'physical_sim' || simFields.some(({ key }) => sim[key] !== ''));
  $effect(() => { session.dirty = dirty; });

  function fill(current: ServiceItem): void {
    entity = current;
    categoryID = current.category_id;
    name = current.name;
    general = { provider: current.provider ?? '', identifier: current.identifier ?? '', management_url: current.management_url ?? '', notes: current.notes ?? '' };
    cardType = current.sim_details?.card_type ?? 'physical_sim';
    sim = { phone_number: current.sim_details?.phone_number ?? '', country_or_region: current.sim_details?.country_or_region ?? '', iccid: current.sim_details?.iccid ?? '' };
    cleared = {};
  }
  async function load(): Promise<void> {
    if (!getInitData() || session.auth === 'expired' || session.auth === 'forbidden') { loading = false; return; }
    loading = true;
    loadError = null;
    try {
      const [allCategories, current] = await Promise.all([
        fetchAllPages((after) => api.categories(after, controller.signal)),
        id ? api.service(id, controller.signal) : Promise.resolve(null),
      ]);
      if (!session.me) await api.me(controller.signal).then((user) => { if (alive && !controller.signal.aborted) acceptMe(user); });
      if (!alive || controller.signal.aborted) return;
      categories = allCategories;
      if (current) fill(current);
      else {
        await tick();
        const preset = initialKind === 'sim' ? allCategories.find((item) => item.code === 'sim') : allCategories.find((item) => item.kind === 'generic');
        categoryID = preset?.id ?? '';
        initialCategoryID = categoryID;
      }
    } catch (cause) { if (alive && !controller.signal.aborted) loadError = cause; }
    finally { if (alive) loading = false; }
  }
  onMount(() => {
    const unsubscribe = onSessionFailure(() => { controller.abort(); loading = false; });
    void load();
    return unsubscribe;
  });
  onDestroy(() => { alive = false; controller.abort(); session.dirty = false; });

  function validManagementURL(value: string): boolean {
    if (!value) return true;
    if (Array.from(value).length > 2048 || !/^https?:\/\/[^\s/@]+(?:[/?#]|$)/.test(value) || /^[^/]*\/\/[^/?#]*@/.test(value)) return false;
    try {
      const url = new URL(value);
      return (url.protocol === 'http:' || url.protocol === 'https:') && Boolean(url.hostname) && !url.username && !url.password;
    } catch { return false; }
  }
  function validate(): boolean {
    const next: Record<string, string> = {};
    if (!name.trim()) next.name = '请输入服务名称';
    else if (Array.from(name).length > 200) next.name = '名称最多 200 个字符';
    if (!categoryID || !category) next.category_id = '请选择类别';
    else if (entityKind && category.kind !== entityKind) next.category_id = '编辑时不能更改服务类型';
    for (const field of generalFields) {
      if (Array.from(general[field.key]).length > field.limit) next[field.key] = `最多 ${field.limit} 个字符`;
    }
    if (!validManagementURL(general.management_url)) next.management_url = '请输入无账号密码的绝对 HTTP 或 HTTPS 链接';
    if (kind === 'sim') for (const field of simFields) if (Array.from(sim[field.key]).length > 200) next[field.key] = '最多 200 个字符';
    errors = next;
    return Object.keys(next).length === 0;
  }
  async function saveCategory(): Promise<void> {
    if (!canWrite() || categoryBusy || submitter.busy || categoryMode === 'closed') return;
    const trimmed = categoryName.trim();
    if (!trimmed || Array.from(trimmed).length > 200) { categoryError = '类别名称必须为 1 至 200 个字符'; return; }
    categoryBusy = true;
    categoryError = '';
    const mode = categoryMode;
    const target = category;
    try {
      const saved = mode === 'rename' && target
        ? await api.updateCategory(target.id, { name: trimmed }, controller.signal)
        : await api.createCategory({ name: trimmed }, controller.signal);
      if (!alive) return;
      categories = mode === 'rename' ? categories.map((item) => item.id === saved.id ? saved : item) : [...categories, saved].sort((a, b) => BigInt(a.id) < BigInt(b.id) ? -1 : 1);
      categoryID = saved.id;
      categoryName = '';
      categoryMode = 'closed';
    } catch (cause) { if (alive) categoryError = errorMessage(cause); }
    finally { if (alive) categoryBusy = false; }
  }
  function openRename(): void {
    if (!category || category.user_id === null || category.user_id !== session.me?.id) return;
    categoryName = category.name;
    categoryError = '';
    categoryMode = 'rename';
  }
  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!canWrite() || submitter.busy || categoryBusy || categoryMode !== 'closed' || loading || loadError || conflict.open || conflict.failed || !validate()) return;
    await submitter.run(async () => {
      const common = {} as ServiceUpdateInput;
      if (!id || name !== entity?.name) common.name = name;
      if (!id || categoryID !== entity?.category_id) common.category_id = categoryID;
      for (const { key } of generalFields) {
        if (id) {
          if (cleared[key]) { if (entity?.[key] !== null) common[key] = null; }
          else if (general[key] !== (entity?.[key] ?? '')) common[key] = general[key] === '' ? null : general[key];
        } else if (general[key] !== '') common[key] = general[key];
      }
      if (kind === 'sim') {
        const details: NonNullable<ServiceUpdateInput['sim_details']> = {};
        if (!id || cardType !== entity?.sim_details?.card_type) details.card_type = cardType;
        for (const { key } of simFields) {
          if (id) {
            if (cleared[key]) { if (entity?.sim_details?.[key] !== null) details[key] = null; }
            else if (sim[key] !== (entity?.sim_details?.[key] ?? '')) details[key] = sim[key] === '' ? null : sim[key];
          } else if (sim[key] !== '') details[key] = sim[key];
        }
        if (Object.keys(details).length) common.sim_details = details;
      }
      try {
        const saved = id
          ? await api.updateService(id, common, controller.signal)
          : await api.createService({ ...common, name, category_id: categoryID, ...(kind === 'sim' ? { sim_details: { card_type: cardType, ...Object.fromEntries(simFields.filter(({ key }) => sim[key] !== '').map(({ key }) => [key, sim[key]])) } } : {}) } as ServiceCreateInput, controller.signal);
        if (!alive) return;
        fill(saved);
        session.dirty = false;
        navigate(`/app/services/${saved.id}`);
      } catch (cause) {
        if (alive && id && cause instanceof ApiError && cause.kind === 'conflict') await conflict.present();
        throw cause;
      }
    });
  }
  function useLatest(): void {
    const current = conflict.current;
    const previous = entity;
    if (!current || conflict.loading || conflict.failed) return;
    const draft = { name, categoryID, general: { ...general }, sim: { ...sim }, cardType, cleared: { ...cleared } };
    fill(current);
    if (draft.name !== (previous?.name ?? '')) name = draft.name;
    if (draft.categoryID !== (previous?.category_id ?? '')) categoryID = draft.categoryID;
    for (const { key } of generalFields) if (draft.cleared[key] || draft.general[key] !== (previous?.[key] ?? '')) { general[key] = draft.general[key]; cleared[key] = draft.cleared[key] ?? false; }
    if (draft.cardType !== (previous?.sim_details?.card_type ?? 'physical_sim')) cardType = draft.cardType;
    for (const { key } of simFields) if (draft.cleared[key] || draft.sim[key] !== (previous?.sim_details?.[key] ?? '')) { sim[key] = draft.sim[key]; cleared[key] = draft.cleared[key] ?? false; }
    errors = {};
    submitter.clear();
    conflict.dismiss();
  }
</script>

<section class="content stack" aria-labelledby="service-form-title">
  <h1 id="service-form-title">{id ? '编辑服务' : '添加服务'}</h1>
  {#if loading}<p role="status">{labels.loading}</p>
  {:else if loadError}<ErrorBanner message={errorMessage(loadError)} onretry={session.auth === 'ready' ? load : undefined} />
  {:else}
    {#if !canWrite()}<p class="notice">{labels.readOnly}</p>{/if}
    <form class="stack" onsubmit={submit} novalidate>
      <FormField id="service-name" label="服务名称" required error={errors.name}>
        {#snippet children(field)}<input id={field.id} bind:value={name} maxlength="200" disabled={!canWrite() || submitter.busy} required aria-invalid={field.invalid} aria-describedby={field.describedby} />{/snippet}
      </FormField>
      <FormField id="service-category" label="类别" required error={errors.category_id}>
        {#snippet children(field)}
          <select id={field.id} bind:value={categoryID} disabled={!canWrite() || submitter.busy || categoryBusy || categoryMode !== 'closed'} required aria-invalid={field.invalid} aria-describedby={field.describedby}>
            <option value="">请选择类别</option>
            {#each categories.filter((item) => !entityKind || item.kind === entityKind) as item (item.id)}<option value={item.id}>{item.name}</option>{/each}
          </select>
        {/snippet}
        {#snippet action()}
          {#if category && category.user_id !== null && category.user_id === session.me?.id}<button type="button" class="text-button" onclick={openRename} disabled={!canWrite() || submitter.busy || categoryBusy}>重命名</button>{/if}
        {/snippet}
      </FormField>
      {#if entityKind !== 'sim'}
      <div class="actions wrap">
        <button type="button" onclick={() => { categoryMode = categoryMode === 'create' ? 'closed' : 'create'; categoryName = ''; categoryError = ''; }} disabled={!canWrite() || submitter.busy || categoryBusy}>新建类别</button>
      </div>
      {/if}
      {#if categoryMode !== 'closed'}
        <div class="inline-category stack">
          <FormField id="category-name" label={categoryMode === 'rename' ? '类别名称' : '新类别名称'} required error={categoryError}>
            {#snippet children(field)}<input id={field.id} bind:value={categoryName} maxlength="200" disabled={categoryBusy} onkeydown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void saveCategory(); } }} />{/snippet}
          </FormField>
          <div class="actions"><button type="button" class="primary" onclick={saveCategory} disabled={categoryBusy || !canWrite()}>{categoryBusy ? '保存中…' : '保存类别'}</button><button type="button" onclick={() => { categoryMode = 'closed'; categoryError = ''; }} disabled={categoryBusy}>取消</button></div>
        </div>
      {/if}
      {#if categoryError && categoryMode === 'closed'}<ErrorBanner message={categoryError} />{/if}
      {#if category}
        <FormField id="service-provider" label={kind === 'sim' ? '运营商' : serviceFields.provider} error={errors.provider}>
          {#snippet action()}{#if general.provider}<button type="button" class="text-button" aria-label={kind === 'sim' ? '清除运营商' : '清除服务商'} disabled={!canWrite() || submitter.busy} onclick={() => { general.provider = ''; cleared.provider = Boolean(id); }}>清除</button>{/if}{/snippet}
          {#snippet children(field)}<input id={field.id} bind:value={general.provider} maxlength="200" oninput={() => { cleared.provider = false; }} disabled={!canWrite() || submitter.busy} aria-invalid={field.invalid} aria-describedby={field.describedby} />{/snippet}
        </FormField>
        <FormField id="service-identifier" label={serviceFields.identifier} error={errors.identifier}>
          {#snippet action()}{#if general.identifier}<button type="button" class="text-button" aria-label="清除公开标识" disabled={!canWrite() || submitter.busy} onclick={() => { general.identifier = ''; cleared.identifier = Boolean(id); }}>清除</button>{/if}{/snippet}
          {#snippet children(field)}<input id={field.id} bind:value={general.identifier} maxlength="200" oninput={() => { cleared.identifier = false; }} disabled={!canWrite() || submitter.busy} aria-invalid={field.invalid} aria-describedby={field.describedby} />{/snippet}
        </FormField>
        <FormField id="service-url" label="管理链接" error={errors.management_url}>
          {#snippet action()}{#if general.management_url}<button type="button" class="text-button" aria-label="清除管理链接" disabled={!canWrite() || submitter.busy} onclick={() => { general.management_url = ''; cleared.management_url = Boolean(id); }}>清除</button>{/if}{/snippet}
          {#snippet children(field)}<input id={field.id} type="url" bind:value={general.management_url} maxlength="2048" oninput={() => { cleared.management_url = false; }} disabled={!canWrite() || submitter.busy} aria-invalid={field.invalid} aria-describedby={field.describedby} />{/snippet}
        </FormField>
        {#if kind === 'sim'}
          <FormField id="service-card-type" label="卡片类型" required>
            {#snippet children(field)}<select id={field.id} bind:value={cardType} disabled={!canWrite() || submitter.busy}><option value="physical_sim">{cardTypeLabels.physical_sim}</option><option value="esim">{cardTypeLabels.esim}</option></select>{/snippet}
          </FormField>
          {#each simFields as field (field.key)}
            <FormField id={`service-${field.key}`} label={field.label} error={errors[field.key]}>
              {#snippet action()}{#if sim[field.key]}<button type="button" class="text-button" aria-label={`清除${field.label}`} disabled={!canWrite() || submitter.busy} onclick={() => { sim[field.key] = ''; cleared[field.key] = Boolean(id); }}>清除</button>{/if}{/snippet}
              {#snippet children(input)}<input id={input.id} bind:value={sim[field.key]} maxlength="200" oninput={() => { cleared[field.key] = false; }} disabled={!canWrite() || submitter.busy} aria-invalid={input.invalid} aria-describedby={input.describedby} />{/snippet}
            </FormField>
          {/each}
        {/if}
        <FormField id="service-notes" label="备注" error={errors.notes}>
          {#snippet action()}{#if general.notes}<button type="button" class="text-button" aria-label="清除备注" disabled={!canWrite() || submitter.busy} onclick={() => { general.notes = ''; cleared.notes = Boolean(id); }}>清除</button>{/if}{/snippet}
          {#snippet children(field)}<textarea id={field.id} rows="4" bind:value={general.notes} maxlength="4000" oninput={() => { cleared.notes = false; }} disabled={!canWrite() || submitter.busy} aria-invalid={field.invalid} aria-describedby={field.describedby}></textarea>{/snippet}
        </FormField>
        <p class="muted">{labels.secretsWarning}</p>
      {/if}
      {#if submitter.error && !conflict.open}<ErrorBanner message={errorMessage(submitter.error)} />{/if}
      {#if conflict.failed && !conflict.open}<ErrorBanner message="无法读取最新资料，提交已暂停。" onretry={() => { void conflict.retry(); }} />{/if}
      <button type="submit" class="primary" disabled={!canWrite() || submitter.busy || categoryBusy || categoryMode !== 'closed' || conflict.open || conflict.failed || (Boolean(id) && !dirty)}>{submitter.busy ? '保存中…' : '保存服务'}</button>
    </form>
  {/if}
</section>
<ConfirmDialog bind:open={conflict.open} title="服务资料已变化" message="草稿已保留。取最新值将更新未修改的字段；请重新检查、编辑后再次保存，不会自动提交。" confirmLabel="取最新值继续编辑" busy={conflict.loading} onconfirm={useLatest} oncancel={() => conflict.dismiss()}>
  {#if conflict.loading}<p role="status">正在读取最新资料…</p>
  {:else if conflict.failed}<ErrorBanner message="最新资料读取失败，提交已暂停。" onretry={() => { void conflict.retry(); }} />
  {:else if conflict.current}<p>服务端名称：{conflict.current.name}</p><p>类别 ID：{conflict.current.category_id}</p><p>最后更新：{conflict.current.updated_at}</p>{/if}
</ConfirmDialog>

<style>
  .muted{color:var(--text-secondary);margin:0}
  .inline-category{padding:12px;border:1px solid var(--border);border-radius:8px}
  .wrap{flex-wrap:wrap}
</style>
