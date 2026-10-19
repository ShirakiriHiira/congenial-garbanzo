<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '../lib/api/resources';
  import { ApiError } from '../lib/api/errors';
  import type { DatePrecision, Period, PeriodCreateInput, PeriodUpdateInput, PeriodTermsInput, TermsMode, IntervalUnit } from '../lib/api/types';
  import { ConflictFlow } from '../lib/conflict.svelte';
  import { Submitter } from '../lib/form.svelte';
  import { acceptMe, canWrite, session } from '../lib/session.svelte';
  import { navigate } from '../lib/router';
  import { getInitData } from '../lib/telegram/sdk';
  import { instantToWallTime, isValidDate, wallTimeToRFC3339, type Occurrence } from '../lib/domain/datetime';
  import { formatExpiry } from '../lib/domain/expiry';
  import { formatMoneyInput, parseMoney } from '../lib/domain/money';
  import { errorMessage, intervalUnitLabels, periodLabel, labels, termsModeLabels } from '../lib/domain/labels';
  import MoneyInput from '../components/MoneyInput.svelte';
  import FormField from '../components/FormField.svelte';
  import DatePrecisionInput from '../components/DatePrecisionInput.svelte';
  import DateTimeInput from '../components/DateTimeInput.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import Toast from '../components/Toast.svelte';

  let { id, mode }: { id: string; mode: 'new' | 'edit' } = $props();
  type Snapshot = { name: string; precision: DatePrecision; expires: string; occurrence?: Occurrence; starts: string; startsOccurrence?: Occurrence; terms: PeriodTermsInput };
  let entity = $state<Period | null>(null);
  let zone = $state('');
  let name = $state('');
  let termsMode = $state<TermsMode>('custom');
  let intervalCount = $state('');
  let intervalUnit = $state<IntervalUnit>('month');
  let amount = $state('');
  let currency = $state('');
  let unsafeMoney = $state(false);
  let unsafeTermsReset = $state(false);
  let termsError = $state('');
  let intervalError = $state('');
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
  function readTerms(): PeriodTermsInput {
    const base = { mode: termsMode, interval_count: termsMode === 'recurring' ? Number(intervalCount) : null, interval_unit: termsMode === 'recurring' ? intervalUnit : null };
    try {
      const money = parseMoney(amount, currency);
      return { ...base, amount: money.amount ?? null, currency: money.currency ?? null };
    } catch {
      return { ...base, amount: Number.NaN, currency: currency === '' ? null : currency };
    }
  }
  function isExplicitUnsafeReset(terms: PeriodTermsInput): boolean {
    return unsafeTermsReset && terms.mode === 'custom' && terms.interval_count === null && terms.interval_unit === null && terms.amount === null && terms.currency === null;
  }
  function sameTerms(a: PeriodTermsInput, b: PeriodTermsInput): boolean {
    const unsafeUneditedAmount = !isExplicitUnsafeReset(a) && a.amount === null && a.currency === null && b.amount !== null && !Number.isSafeInteger(b.amount);
    return a.mode === b.mode && a.interval_count === b.interval_count && a.interval_unit === b.interval_unit && (a.amount === b.amount || unsafeUneditedAmount) && (a.currency === b.currency || unsafeUneditedAmount);
  }
  function fillTerms(terms: PeriodTermsInput): void {
    termsMode = terms.mode;
    intervalCount = terms.interval_count === null ? '' : String(terms.interval_count);
    intervalUnit = terms.interval_unit ?? 'month';
    unsafeMoney = terms.amount !== null && !Number.isSafeInteger(terms.amount);
    unsafeTermsReset = false;
    amount = terms.amount === null || unsafeMoney ? '' : formatMoneyInput(terms.amount, terms.currency);
    currency = amount === '' ? '' : terms.currency ?? '';
  }
  let alive = true;
  const controller = new AbortController();
  const submitter = new Submitter();
  const conflict = new ConflictFlow<Period>(() => api.period(id, controller.signal));
  const dirty = $derived(baseline
    ? name !== baseline.name || precision !== baseline.precision || expires !== baseline.expires || occurrence !== baseline.occurrence || starts !== baseline.starts || startsOccurrence !== baseline.startsOccurrence || !sameTerms(readTerms(), baseline.terms)
    : name !== '' || expires !== '' || starts !== '' || precision !== 'date' || termsMode !== 'custom' || amount !== '' || currency !== '');
  $effect(() => { session.dirty = dirty; });
  $effect(() => { expires; precision; occurrence; expiresError = ''; });
  $effect(() => { starts; startsOccurrence; startsError = ''; });

  function snapshotOf(current: Period): Snapshot {
    const resolvedPrecision = current.date_precision;
    const resolvedExpires = resolvedPrecision === 'date' ? current.local_expires_date ?? '' : instantToWallTime(current.expires_at, current.timezone);
    const resolvedStarts = current.starts_at ? instantToWallTime(current.starts_at, current.timezone) : null;
    return {
      name: current.name, precision: resolvedPrecision, terms: { mode: current.terms.mode, interval_count: current.terms.interval_count, interval_unit: current.terms.interval_unit, amount: current.terms.amount, currency: current.terms.currency },
      expires: typeof resolvedExpires === 'string' ? resolvedExpires : resolvedExpires.value,
      occurrence: typeof resolvedExpires === 'string' ? undefined : resolvedExpires.occurrence,
      starts: resolvedStarts?.value ?? '', startsOccurrence: resolvedStarts?.occurrence,
    };
  }
  function fill(current: Period) {
    entity = current;
    zone = current.timezone;
    name = current.name;
    precision = current.date_precision;
    const resolved = snapshotOf(current);
    expires = resolved.expires;
    fillTerms(resolved.terms);
    occurrence = resolved.occurrence;
    if (current.starts_at) { const wall = instantToWallTime(current.starts_at, zone); starts = wall.value; startsOccurrence = wall.occurrence; }
    else { starts = ''; startsOccurrence = undefined; }
    baseline = resolved;
  }
  async function load() {
    loading = true;
    loadError = null;
    if (!getInitData() || session.auth === 'expired' || session.auth === 'forbidden') { loading = false; return; }
    try {
      if (mode === 'edit') {
        const [current, user] = await Promise.all([api.period(id, controller.signal), session.me ? Promise.resolve(session.me) : api.me(controller.signal)]);
        if (alive) { acceptMe(user); fill(current); }
      } else {
        const user = session.me ? session.me : await api.me(controller.signal);
        if (alive) { acceptMe(user); zone = user.timezone; }
      }
    } catch (cause) { if (alive && !controller.signal.aborted) loadError = cause; }
    finally { if (alive) loading = false; }
  }
  onMount(() => { void load(); });
  onDestroy(() => { alive = false; controller.abort(); session.dirty = false; });

  function prepare(): { expiresValue: string; startsValue?: string } | null {
    nameError = Array.from(name).length > 200 ? '名称最多 200 个字符' : '';
    expiresError = startsError = termsError = '';
    intervalError = termsMode === 'recurring' && (!/^\d+$/.test(intervalCount) || !Number.isSafeInteger(Number(intervalCount)) || Number(intervalCount) <= 0) ? '周期次数必须是正整数' : '';
    let expiresValue = '';
    let startsValue: string | undefined;
    try {
      parseMoney(amount, currency);
      if (unsafeMoney && baseline && amount === '' && !isExplicitUnsafeReset(readTerms()) && !sameTerms(readTerms(), baseline.terms)) throw new Error('原约定费用无法精确显示，请重新填写金额和币种后再修改条款');
    } catch (cause) { termsError = cause instanceof Error ? cause.message : '条款无效'; }
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
    if (nameError || expiresError || startsError || termsError || intervalError) return null;
    return { expiresValue, startsValue };
  }
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!canWrite() || submitter.busy || loading || loadError || stale || conflict.open || conflict.failed) return;
    const prepared = prepare();
    if (!prepared) return;
    await submitter.run(async () => {
      try {
        let saved: Period;
        if (mode === 'new') {
          const body: PeriodCreateInput = { name, date_precision: precision, expires_at: prepared.expiresValue, terms: readTerms() };
          if (prepared.startsValue) body.starts_at = prepared.startsValue;
          saved = await api.createPeriod(id, body, controller.signal);
        } else {
          if (!entity || !baseline) return;
          const body: PeriodUpdateInput = { expected_cycle: entity.cycle };
          if (!sameTerms(readTerms(), baseline.terms)) { body.terms = readTerms(); body.expected_terms_revision = entity.terms.revision; }
          if (name !== baseline.name) body.name = name;
          if (precision !== baseline.precision) { body.date_precision = precision; body.expires_at = prepared.expiresValue; }
          else if (expires !== baseline.expires || occurrence !== baseline.occurrence) body.expires_at = prepared.expiresValue;
          if (starts !== baseline.starts || startsOccurrence !== baseline.startsOccurrence) body.starts_at = prepared.startsValue ?? null;
          saved = await api.updatePeriod(id, body, controller.signal);
        }
        if (!alive) return;
        fill(saved);
        session.dirty = false;
        if (mode === 'new') navigate(`/app/services/${saved.service_id}`);
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
    zone = current.timezone;
    const resolved = snapshotOf(current);
    if (oldBaseline) {
      if (name === oldBaseline.name) name = resolved.name;
      if (precision === oldBaseline.precision) precision = resolved.precision;
      if (expires === oldBaseline.expires && occurrence === oldBaseline.occurrence) { expires = resolved.expires; occurrence = resolved.occurrence; }
      if (starts === oldBaseline.starts && startsOccurrence === oldBaseline.startsOccurrence) { starts = resolved.starts; startsOccurrence = resolved.startsOccurrence; }
      if (sameTerms(readTerms(), oldBaseline.terms)) fillTerms(resolved.terms);
      if (isExplicitUnsafeReset(readTerms()) && sameTerms(readTerms(), resolved.terms)) { unsafeTermsReset = false; unsafeMoney = resolved.terms.amount !== null && !Number.isSafeInteger(resolved.terms.amount); }
    }
    baseline = resolved;
    stale = false;
    nameError = expiresError = startsError = termsError = intervalError = '';
    submitter.clear();
    conflict.dismiss();
  }
</script>

<section class="content stack" aria-labelledby="period-form-title">
  <h1 id="period-form-title">{mode === 'new' ? '添加有效期' : '编辑有效期'}</h1>
  {#if loading}<p role="status">{labels.loading}</p>
  {:else if loadError}<ErrorBanner message={errorMessage(loadError)} onretry={load} />
  {:else if zone}
    <p class="muted">时区：{zone}</p>
    {#if !canWrite()}<p class="notice">{labels.readOnly}</p>{/if}
    <Toast message={notice} ondismiss={() => { notice = ''; }} />
    <form class="stack" onsubmit={submit} novalidate>
      <FormField id="period-name" label="有效期名称" hint="可选" error={nameError}>
        {#snippet children(field)}<input id={field.id} bind:value={name} disabled={!canWrite() || submitter.busy} aria-invalid={field.invalid} aria-describedby={field.describedby} />{/snippet}
      </FormField>
      <DatePrecisionInput id="period-expires" bind:precision bind:expires bind:occurrence {zone} disabled={!canWrite() || submitter.busy} error={expiresError} />
      <DateTimeInput id="period-starts" label="开始时间（可选）" bind:value={starts} bind:occurrence={startsOccurrence} {zone} disabled={!canWrite() || submitter.busy} error={startsError} />
      <fieldset class="terms stack"><legend>约定条款</legend>
        <FormField id="terms-mode" label="条款类型" required>{#snippet children(field)}<select id={field.id} bind:value={termsMode} disabled={!canWrite() || submitter.busy}>{#each Object.entries(termsModeLabels) as [value, label]}<option {value}>{label}</option>{/each}</select>{/snippet}</FormField>
        {#if termsMode === 'recurring'}<FormField id="terms-count" label="周期次数" required error={intervalError}>{#snippet children(field)}<input id={field.id} type="text" inputmode="numeric" pattern="[0-9]*" bind:value={intervalCount} disabled={!canWrite() || submitter.busy} />{/snippet}</FormField><FormField id="terms-unit" label="周期单位" required>{#snippet children(field)}<select id={field.id} bind:value={intervalUnit} disabled={!canWrite() || submitter.busy}>{#each Object.entries(intervalUnitLabels) as [value, label]}<option {value}>{label}</option>{/each}</select>{/snippet}</FormField>{/if}
        <MoneyInput id="period-terms-money" label="约定费用" bind:amount bind:currency disabled={!canWrite() || submitter.busy} error={termsError} />
        {#if unsafeMoney && isExplicitUnsafeReset(readTerms())}<p class="notice">保存后将把约定条款重置为自定义且无费用；原金额不会被四舍五入写入。</p>
        {:else if unsafeMoney}<div class="unsafe-money"><p class="notice">{"已保存的约定费用无法精确编辑；保留空白可保存其他资料，修改条款请重新填写金额和币种，或明确重置为无费用条款。"}</p><button class="text-button" type="button" onclick={() => { termsMode = 'custom'; intervalCount = ''; intervalUnit = 'month'; amount = ''; currency = ''; termsError = ''; unsafeTermsReset = true; }} disabled={!canWrite() || submitter.busy}>重置约定条款</button></div>{/if}
      </fieldset>
      {#if submitter.error && !conflict.open}<ErrorBanner message={errorMessage(submitter.error)} />{/if}
      {#if stale && !conflict.open}<ErrorBanner message="草稿已保留。请先读取最新值，再继续编辑。" onretry={() => { void conflict.retry(); }} />{/if}
      <button class="primary" type="submit" disabled={!canWrite() || submitter.busy || stale || conflict.open || conflict.failed || (mode === 'edit' && !dirty)}>{submitter.busy ? '保存中…' : '保存有效期'}</button>
    </form>
  {/if}
</section>
<ConfirmDialog bind:open={conflict.open} title="有效期周期冲突" message="草稿已保留。服务端记录已变化；取最新值后请重新编辑并再次保存，不会自动提交或合并。" confirmLabel="取最新值继续编辑" busy={conflict.loading} onconfirm={useLatest} oncancel={() => conflict.dismiss()}>
  {#if conflict.loading}<p role="status">正在读取最新有效期…</p>
  {:else if conflict.failed}<ErrorBanner message="读取最新有效期失败，保存已暂停。" onretry={() => { void conflict.retry(); }} />
  {:else if conflict.current}<p>服务端名称：{periodLabel(conflict.current)}</p><p>服务端截止：{formatExpiry(conflict.current)}</p>{/if}
</ConfirmDialog>
<style>.muted{color:var(--text-secondary);margin:0}.terms{min-width:0;margin:0;padding:12px;border:1px solid var(--border);border-radius:8px}.terms legend{padding:0 4px;font-weight:600}.unsafe-money{display:grid;justify-items:start;gap:4px}.unsafe-money .text-button{margin-block:0}</style>
