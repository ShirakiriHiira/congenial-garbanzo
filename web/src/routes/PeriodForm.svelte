<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../lib/api/resources';
  import { ApiError } from '../lib/api/errors';
  import type { DatePrecision, Period, PeriodType, PeriodCreateInput, PeriodUpdateInput } from '../lib/api/types';
  import { ConflictFlow } from '../lib/conflict.svelte';
  import { Submitter } from '../lib/form.svelte';
  import { acceptMe, canWrite, session } from '../lib/session.svelte';
  import { navigate } from '../lib/router';
  import { getInitData } from '../lib/telegram/sdk';
  import { instantToWallTime, isValidDate, wallTimeToRFC3339, type Occurrence } from '../lib/domain/datetime';
  import { formatExpiry } from '../lib/domain/expiry';
  import { errorMessage, periodTypeLabels, labels } from '../lib/domain/labels';
  import FormField from '../components/FormField.svelte';
  import DatePrecisionInput from '../components/DatePrecisionInput.svelte';
  import DateTimeInput from '../components/DateTimeInput.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import Toast from '../components/Toast.svelte';

  let { id, mode }: { id: string; mode: 'new' | 'edit' } = $props();
  type Snapshot = { name: string; type: PeriodType; precision: DatePrecision; expires: string; occurrence?: Occurrence; starts: string; startsOccurrence?: Occurrence };
  let entity = $state<Period | null>(null);
  let zone = $state('');
  let name = $state('');
  let periodType = $state<PeriodType>('number_validity');
  let precision = $state<DatePrecision>('date');
  let expires = $state('');
  let occurrence = $state<Occurrence | undefined>();
  let starts = $state('');
  let startsOccurrence = $state<Occurrence | undefined>();
  let baseline = $state<Snapshot | null>(null);
  let loading = $state(true);
  let loadError = $state<unknown>(null);
  let nameError = $state('');
  let expiresError = $state('');
  let startsError = $state('');
  let notice = $state('');
  let stale = $state(false);
  let alive = true;
  const controller = new AbortController();
  const submitter = new Submitter();
  const conflict = new ConflictFlow<Period>(() => api.period(id, controller.signal));
  const dirty = $derived(baseline
    ? name !== baseline.name || periodType !== baseline.type || precision !== baseline.precision || expires !== baseline.expires || occurrence !== baseline.occurrence || starts !== baseline.starts || startsOccurrence !== baseline.startsOccurrence
    : name !== '' || expires !== '' || starts !== '' || precision !== 'date' || periodType !== 'number_validity');
  $effect(() => { session.dirty = dirty; });
  $effect(() => { expires; precision; occurrence; expiresError = ''; });
  $effect(() => { starts; startsOccurrence; startsError = ''; });

  function snapshotOf(current: Period): Snapshot {
    const resolvedPrecision = current.date_precision;
    const resolvedExpires = resolvedPrecision === 'date' ? current.local_expires_date ?? '' : instantToWallTime(current.expires_at, current.timezone);
    const resolvedStarts = current.starts_at ? instantToWallTime(current.starts_at, current.timezone) : null;
    return {
      name: current.name, type: current.period_type, precision: resolvedPrecision,
      expires: typeof resolvedExpires === 'string' ? resolvedExpires : resolvedExpires.value,
      occurrence: typeof resolvedExpires === 'string' ? undefined : resolvedExpires.occurrence,
      starts: resolvedStarts?.value ?? '', startsOccurrence: resolvedStarts?.occurrence,
    };
  }
  function snapshot(): Snapshot {
    return { name, type: periodType, precision, expires, occurrence, starts, startsOccurrence };
  }
  function fill(current: Period) {
    entity = current;
    zone = current.timezone;
    name = current.name;
    periodType = current.period_type;
    precision = current.date_precision;
    const resolved = snapshotOf(current);
    expires = resolved.expires;
    occurrence = resolved.occurrence;
    if (current.starts_at) { const wall = instantToWallTime(current.starts_at, zone); starts = wall.value; startsOccurrence = wall.occurrence; }
    else { starts = ''; startsOccurrence = undefined; }
    baseline = snapshot();
  }
  async function load() {
    loading = true;
    loadError = null;
    if (!getInitData() || session.auth === 'expired' || session.auth === 'forbidden') { loading = false; return; }
    try {
      if (mode === 'edit') {
        const [current] = await Promise.all([
          api.period(id, controller.signal),
          session.me ? Promise.resolve() : api.me(controller.signal).then(acceptMe),
        ]);
        if (alive) fill(current);
      } else {
        const user = session.me ?? await api.me(controller.signal);
        if (alive) { acceptMe(user); zone = user.timezone; }
      }
    } catch (cause) { if (alive && !controller.signal.aborted) loadError = cause; }
    finally { if (alive) loading = false; }
  }
  onMount(() => { void load(); });
  onDestroy(() => { alive = false; controller.abort(); session.dirty = false; });

  function prepare(): { expiresValue: string; startsValue?: string } | null {
    nameError = Array.from(name).length > 200 ? '名称最多 200 个字符' : '';
    expiresError = startsError = '';
    let expiresValue = '';
    let startsValue: string | undefined;
    try {
      if (!expires) throw new Error(entity && precision !== entity.date_precision ? '切换精度后，请重新填写截止日期或时间' : '请填写截止日期或时间');
      if (precision === 'date') {
        if (!isValidDate(expires)) throw new Error('请输入有效的截止日期');
        expiresValue = expires;
      } else expiresValue = wallTimeToRFC3339(expires, zone, occurrence);
    } catch (cause) { expiresError = cause instanceof Error ? cause.message : '截止日期或时间无效'; }
    if (starts) {
      try { startsValue = wallTimeToRFC3339(starts, zone, startsOccurrence); }
      catch (cause) { startsError = cause instanceof Error ? cause.message : '开始时间无效'; }
    }
    if (nameError || expiresError || startsError) return null;
    return { expiresValue, startsValue };
  }
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!canWrite() || submitter.busy || loading || loadError || stale || conflict.open || conflict.failed || entity?.status === 'cancelled') return;
    const prepared = prepare();
    if (!prepared) return;
    await submitter.run(async () => {
      try {
        let saved: Period;
        if (mode === 'new') {
          const body: PeriodCreateInput = { name, period_type: periodType, date_precision: precision, expires_at: prepared.expiresValue };
          if (prepared.startsValue) body.starts_at = prepared.startsValue;
          saved = await api.createPeriod(id, body, controller.signal);
        } else {
          if (!entity || !baseline) return;
          const body: PeriodUpdateInput = { expected_cycle: entity.cycle };
          if (name !== baseline.name) body.name = name;
          if (periodType !== baseline.type) body.period_type = periodType;
          if (precision !== baseline.precision) { body.date_precision = precision; body.expires_at = prepared.expiresValue; }
          else if (expires !== baseline.expires || occurrence !== baseline.occurrence) body.expires_at = prepared.expiresValue;
          if (starts !== baseline.starts || startsOccurrence !== baseline.startsOccurrence) body.starts_at = prepared.startsValue ?? null;
          saved = await api.updatePeriod(id, body, controller.signal);
        }
        if (!alive) return;
        fill(saved);
        session.dirty = false;
        if (mode === 'new') navigate(`/app/sims/${saved.sim_id}`);
        else notice = '有效期已保存，页面已使用服务端返回的最新周期。';
      } catch (cause) {
        if (alive && mode === 'edit' && cause instanceof ApiError && cause.kind === 'conflict') { stale = true; await conflict.present(); }
        throw cause;
      }
    });
  }
  function useLatest() {
    const current = conflict.current;
    const oldBaseline = baseline;
    if (!current || conflict.loading || conflict.failed) return;
    entity = current;
    if (current.status === 'cancelled') {
      stale = true;
      conflict.dismiss();
      return;
    }
    zone = current.timezone;
    const resolved = snapshotOf(current);
    if (oldBaseline) {
      if (name === oldBaseline.name) name = resolved.name;
      if (periodType === oldBaseline.type) periodType = resolved.type;
    }
    baseline = resolved;
    stale = false;
    nameError = expiresError = startsError = '';
    submitter.clear();
    conflict.dismiss();
  }
</script>

<section class="content stack" aria-labelledby="period-form-title">
  <h1 id="period-form-title">{mode === 'new' ? '添加有效期' : '编辑有效期'}</h1>
  {#if loading}<p role="status">{labels.loading}</p>
  {:else if loadError}<ErrorBanner message={errorMessage(loadError)} onretry={load} />
  {:else if entity?.status === 'cancelled'}<p class="notice">该有效期已取消，不可恢复，也不能再编辑。请返回卡片详情。</p>
  {:else if zone}
    <p class="muted">使用时区：{zone}{entity ? ` · 当前周期 ${entity.cycle}` : ''}。修改截止日期或精度会使旧周期失效。</p>
    {#if !canWrite()}<p class="notice">{labels.readOnly}</p>{/if}
    <Toast message={notice} />
    <form class="stack" onsubmit={submit} novalidate>
      <FormField id="period-type" label="有效期类型" required>
        {#snippet children(field)}<select id={field.id} bind:value={periodType} disabled={!canWrite() || submitter.busy}>{#each Object.entries(periodTypeLabels) as [value, label]}<option {value}>{label}</option>{/each}</select>{/snippet}
      </FormField>
      <FormField id="period-name" label="有效期名称" hint="可选，最多 200 个字符" error={nameError}>
        {#snippet children(field)}<input id={field.id} bind:value={name} disabled={!canWrite() || submitter.busy} aria-invalid={field.invalid} aria-describedby={field.describedby} />{/snippet}
      </FormField>
      <DatePrecisionInput id="period-expires" bind:precision bind:expires bind:occurrence {zone} disabled={!canWrite() || submitter.busy} error={expiresError} />
      <DateTimeInput id="period-starts" label="开始时间（可选）" bind:value={starts} bind:occurrence={startsOccurrence} {zone} disabled={!canWrite() || submitter.busy} error={startsError} />
      {#if submitter.error && !conflict.open}<ErrorBanner message={errorMessage(submitter.error)} />{/if}
      {#if stale && !conflict.open}<ErrorBanner message="草稿已保留。请先读取最新值，再继续编辑。" onretry={() => { void conflict.retry(); }} />{/if}
      <button class="primary" type="submit" disabled={!canWrite() || submitter.busy || stale || conflict.open || conflict.failed || (mode === 'edit' && !dirty)}>{submitter.busy ? '保存中…' : '保存有效期'}</button>
    </form>
  {/if}
</section>
<ConfirmDialog bind:open={conflict.open} title="有效期周期冲突" message="草稿已保留。服务端记录已变化；取最新值后请重新编辑并再次保存，不会自动提交或合并。" confirmLabel="取最新值继续编辑" busy={conflict.loading} onconfirm={useLatest} oncancel={() => conflict.dismiss()}>
  {#if conflict.loading}<p role="status">正在读取最新有效期…</p>
  {:else if conflict.failed}<ErrorBanner message="读取最新有效期失败，保存已暂停。" onretry={() => { void conflict.retry(); }} />
  {:else if conflict.current}<p>周期：{entity?.cycle} → {conflict.current.cycle}</p><p>服务端名称：{conflict.current.name}</p><p>服务端截止：{formatExpiry(conflict.current)}</p>{#if conflict.current.status === 'cancelled'}<p>该有效期已取消，不可恢复。取最新值后只能返回。</p>{/if}{/if}
</ConfirmDialog>
<style>.muted{color:var(--text-secondary);margin:0}</style>
