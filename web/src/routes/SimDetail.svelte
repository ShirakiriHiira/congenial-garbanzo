<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../lib/api/resources';
  import { ApiError } from '../lib/api/errors';
  import { ConflictFlow } from '../lib/conflict.svelte';
  import type { Page, Period, Renewal, Rule, Sim } from '../lib/api/types';
  import { acceptMe, canWrite, onSessionFailure, session } from '../lib/session.svelte';
  import { navigate } from '../lib/router';
  import { getInitData } from '../lib/telegram/sdk';
  import { formatDateTime } from '../lib/domain/datetime';
  import { formatExpiry } from '../lib/domain/expiry';
  import { cardTypeLabels, errorMessage, labels, periodStatusLabels, periodTypeLabel, simStatusLabels } from '../lib/domain/labels';
  import { maskPhone } from '../lib/domain/mask';
  import { formatMoney } from '../lib/domain/money';
  import AsyncState from '../components/AsyncState.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import ExpiryBadge from '../components/ExpiryBadge.svelte';
  import IconTile from '../components/IconTile.svelte';
  import SectionHeader from '../components/SectionHeader.svelte';
  import Toast from '../components/Toast.svelte';

  let { id }: { id: string } = $props();
  interface Section {
    rules: Page<Rule> | null;
    renewals: Page<Renewal> | null;
    rulesLoading: boolean;
    renewalsLoading: boolean;
    rulesError: unknown;
    renewalsError: unknown;
  }
  let sim = $state<Sim | null>(null);
  let simLoading = $state(true);
  let simError = $state<unknown>(null);
  let periods = $state<Period[]>([]);
  let nextCursor = $state<string | null>(null);
  let periodLoading = $state(true);
  let periodError = $state<unknown>(null);
  let loadingMore = $state(false);
  let moreError = $state<unknown>(null);
  let sections = $state<Record<string, Section>>({});
  let expanded = $state<Record<string, boolean>>({});
  let actionError = $state('');
  let notice = $state('');
  let actionBusy = $state(false);
  let archiveOpen = $state(false);
  let restoreOpen = $state(false);
  let deleteSimOpen = $state(false);
  let deletePeriodOpen = $state(false);
  let deleteTarget = $state<Period | null>(null);
  let conflictPeriodID = $state('');
  let conflictPreviousCycle = $state(0);
  const deleteConflict = new ConflictFlow<Period>(() => api.period(conflictPeriodID, controller.signal));
  let alive = true;
  let controller = new AbortController();
  const terminal = $derived(session.auth === 'expired' || session.auth === 'forbidden');

  function stop(): AbortController {
    controller.abort();
    controller = new AbortController();
    return controller;
  }
  function section(periodID: string): Section {
    return sections[periodID] ?? (sections[periodID] = { rules: null, renewals: null, rulesLoading: false, renewalsLoading: false, rulesError: null, renewalsError: null });
  }
  async function fetchSim(signal: AbortSignal): Promise<void> {
    try {
      const current = await api.sim(id, signal);
      if (alive && !signal.aborted) {
        sim = current;
        simError = null;
      }
    } catch (error) {
      if (alive && !signal.aborted) {
        simError = error;
      }
    } finally {
      if (alive && !signal.aborted) simLoading = false;
    }
  }
  async function loadPeriods(reset: boolean): Promise<void> {
    if (terminal) return;
    const signal = controller.signal;
    const cursor = reset ? null : nextCursor;
    if (!reset && cursor === null) return;
    if (reset) {
      periodLoading = true;
      periodError = null;
      moreError = null;
    } else {
      loadingMore = true;
      moreError = null;
    }
    try {
      const page = await api.periods(id, cursor, signal);
      if (!alive || signal.aborted) return;
      const known = new Set(periods.map((period) => period.id));
      periods = reset ? page.data : [...periods, ...page.data.filter((period) => !known.has(period.id))];
      nextCursor = page.next_cursor;
      periodError = null;
    } catch (error) {
      if (!alive || signal.aborted) return;
      if (reset) {
        periodError = error;
      } else {
        moreError = error;
      }
    } finally {
      if (alive && !signal.aborted) periodLoading = loadingMore = false;
    }
  }
  async function refresh(): Promise<void> {
    if (terminal || getInitData().length === 0) {
      simLoading = periodLoading = false;
      return;
    }
    const signal = stop().signal;
    simLoading = periodLoading = true;
    simError = periodError = moreError = null;
    actionError = '';
    notice = '';
    sections = {};
    expanded = {};
    const account = session.me === null
      ? api.me(signal).then((user) => { if (alive) acceptMe(user); }).catch(() => {})
      : Promise.resolve();
    await Promise.all([account, fetchSim(signal), loadPeriods(true)]);
  }
  async function loadMore(): Promise<void> {
    if (periodLoading || loadingMore || nextCursor === null) return;
    await loadPeriods(false);
  }
  onMount(() => {
    const unsubscribe = onSessionFailure(() => {
      controller.abort();
      simLoading = periodLoading = loadingMore = false;
    });
    void refresh();
    return () => {
      alive = false;
      unsubscribe();
      controller.abort();
    };
  });

  function toggle(period: Period): void {
    const open = !expanded[period.id];
    expanded[period.id] = open;
    if (!open) return;
    const state = section(period.id);
    if (state.rules === null && !state.rulesLoading) void loadRules(period.id, false);
    if (state.renewals === null && !state.renewalsLoading) void loadRenewals(period.id, false);
  }
  async function loadRules(periodID: string, append: boolean): Promise<void> {
    const state = section(periodID);
    if (state.rulesLoading) return;
    const cursor = append ? state.rules?.next_cursor ?? null : null;
    if (append && cursor === null) return;
    state.rulesLoading = true;
    state.rulesError = null;
    const signal = controller.signal;
    try {
      const page = await api.rules(periodID, cursor, signal);
      if (!alive || signal.aborted) return;
      if (append && state.rules) {
        const known = new Set(state.rules.data.map((rule) => rule.id));
        state.rules = { data: [...state.rules.data, ...page.data.filter((rule) => !known.has(rule.id))], next_cursor: page.next_cursor };
      } else {
        state.rules = page;
      }
    } catch (error) {
      if (alive && !signal.aborted) state.rulesError = error;
    } finally {
      if (alive && !signal.aborted) state.rulesLoading = false;
    }
  }
  async function loadRenewals(periodID: string, append: boolean): Promise<void> {
    const state = section(periodID);
    if (state.renewalsLoading) return;
    const cursor = append ? state.renewals?.next_cursor ?? null : null;
    if (append && cursor === null) return;
    state.renewalsLoading = true;
    state.renewalsError = null;
    const signal = controller.signal;
    try {
      const page = await api.renewals(periodID, cursor, signal);
      if (!alive || signal.aborted) return;
      if (append && state.renewals) {
        const known = new Set(state.renewals.data.map((renewal) => renewal.id));
        state.renewals = { data: [...state.renewals.data, ...page.data.filter((renewal) => !known.has(renewal.id))], next_cursor: page.next_cursor };
      } else {
        state.renewals = page;
      }
    } catch (error) {
      if (alive && !signal.aborted) state.renewalsError = error;
    } finally {
      if (alive && !signal.aborted) state.renewalsLoading = false;
    }
  }
  async function reload(): Promise<void> {
    await Promise.all([fetchSim(controller.signal), loadPeriods(true)]);
  }
  async function archive(): Promise<void> {
    if (!sim || actionBusy || !canWrite()) return;
    actionBusy = true;
    actionError = '';
    try {
      await api.archiveSim(sim.id, controller.signal);
      if (!alive) return;
      notice = '卡片已归档，未发送的提醒已取消。';
      await reload();
    } catch (error) {
      if (alive) actionError = errorMessage(error);
    } finally {
      if (alive) {
        actionBusy = false;
        archiveOpen = false;
      }
    }
  }
  async function restore(): Promise<void> {
    if (!sim || actionBusy || !canWrite()) return;
    actionBusy = true;
    actionError = '';
    try {
      const restored = await api.updateSim(sim.id, { status: 'active' }, controller.signal);
      if (!alive) return;
      sim = restored;
      notice = '卡片已恢复。有效期周期已加一，旧的版本已失效。';
      await reload();
    } catch (error) {
      if (alive) actionError = errorMessage(error);
    } finally {
      if (alive) {
        actionBusy = false;
        restoreOpen = false;
      }
    }
  }
  async function deleteSim(): Promise<void> {
    if (!sim || !deleteSimOpen || actionBusy || !canWrite()) return;
    actionBusy = true;
    actionError = '';
    try {
      await api.deleteSim(sim.id, controller.signal);
      if (!alive) return;
      navigate('/app/');
    } catch (error) {
      if (alive) actionError = errorMessage(error);
    } finally {
      if (alive) {
        actionBusy = false;
        deleteSimOpen = false;
      }
    }
  }
  function askDeletePeriod(period: Period): void {
    if (actionBusy || !canWrite() || deleteConflict.changed || deleteConflict.open) return;
    deleteTarget = period;
    deletePeriodOpen = true;
    actionError = '';
  }
  async function deletePeriod(): Promise<void> {
    const target = deleteTarget;
    if (!target || !deletePeriodOpen || actionBusy || !canWrite() || deleteConflict.changed || deleteConflict.open) return;
    actionBusy = true;
    actionError = '';
    try {
      await api.deletePeriod(target.id, target.cycle, controller.signal);
      if (!alive) return;
      stop();
      periods = periods.filter((period) => period.id !== target.id);
      sections = {};
      expanded = {};
      notice = labels.periodDeleted;
      await reload();
    } catch (error) {
      if (alive) {
        if (error instanceof ApiError && error.kind === 'conflict') {
          conflictPeriodID = target.id;
          conflictPreviousCycle = target.cycle;
          deletePeriodOpen = false;
          await deleteConflict.present();
        } else {
          actionError = errorMessage(error);
        }
      }
    } finally {
      if (alive) {
        actionBusy = false;
        deletePeriodOpen = false;
        deleteTarget = null;
      }
    }
  }
  function useLatestPeriod(): void {
    if (!deleteConflict.current || deleteConflict.loading || deleteConflict.failed) return;
    const current = deleteConflict.current;
    periods = periods.map((period) => period.id === current.id ? current : period);
    deleteConflict.current = null;
    deleteConflict.dismiss();
  }
  function renewalDate(renewal: Renewal, which: 'previous' | 'new'): string {
    if (which === 'previous') {
      return renewal.previous_date_precision === 'date'
        ? renewal.previous_local_expires_date ?? '日期不可用'
        : formatDateTime(renewal.previous_expires_at, renewal.timezone);
    }
    return renewal.new_date_precision === 'date'
      ? renewal.new_local_expires_date ?? '日期不可用'
      : formatDateTime(renewal.new_expires_at, renewal.timezone);
  }
</script>

<section class="content stack" aria-labelledby="sim-detail-title">
  {#if simLoading && !sim}
    <AsyncState loading />
  {:else if simError && !sim}
    <ErrorBanner message={errorMessage(simError)} onretry={refresh} />
  {:else if sim}
    <div class="heading">
      <IconTile cardType={sim.card_type} />
      <div class="heading-text">
        <h1 id="sim-detail-title">{sim.name}</h1>
        <p class="muted">{cardTypeLabels[sim.card_type]} · {simStatusLabels[sim.status]}</p>
      </div>
      <button type="button" onclick={() => navigate(`/app/sims/${id}/edit`)} disabled={!canWrite() || actionBusy}>编辑</button>
    </div>
    {#if sim.status === 'archived'}<p class="notice" role="status">此卡片已归档，不计入首页统计，未发送的提醒已取消。资料与历史仍保留，可随时恢复。</p>{/if}
    {#if !canWrite()}<p class="notice">当前为只读模式。</p>{/if}
    {#if terminal}<ErrorBanner message={session.auth === 'expired' ? '会话已过期，请关闭并重新打开 Mini App' : '你没有访问权限，请联系机器人管理员确认白名单。'} />{/if}
    {#if actionError}<ErrorBanner message={actionError} />{/if}
    <Toast message={notice} ondismiss={() => { notice = ''; }} />

    <div class="section stack">
      <SectionHeader title="卡片资料">{#snippet children()}{/snippet}</SectionHeader>
      <dl class="profile">
        {#if sim.operator}<div><dt>运营商</dt><dd>{sim.operator}</dd></div>{/if}
        {#if sim.phone_number}<div><dt>手机号</dt><dd>{maskPhone(sim.phone_number)}</dd></div>{/if}
        {#if sim.country_or_region}<div><dt>国家或地区</dt><dd>{sim.country_or_region}</dd></div>{/if}
        {#if sim.notes}<div><dt>备注</dt><dd class="preserve">{sim.notes}</dd></div>{/if}
        {#if !sim.operator && !sim.phone_number && !sim.country_or_region && !sim.notes}<p class="muted">未填写其他资料。</p>{/if}
      </dl>
    </div>

    <div class="section stack">
      <SectionHeader title="有效期">
        <button type="button" onclick={() => navigate(`/app/sims/${id}/periods/new`)} disabled={!canWrite() || actionBusy || sim.status === 'archived'}>添加有效期</button>
      </SectionHeader>
      {#if periodError && periods.length > 0}
        <ErrorBanner message={errorMessage(periodError)} onretry={() => loadPeriods(true)} />
      {/if}
      <AsyncState loading={periodLoading && periods.length === 0}
        error={periodError && periods.length === 0 ? errorMessage(periodError) : ''}
        empty={!periodLoading && !periodError && periods.length === 0}
        emptyMessage="尚未添加有效期。" onretry={() => loadPeriods(true)}>
        <ul class="periods">
          {#each periods as period (period.id)}
            <li class="period">
              <div class="period-head">
                <div class="period-text">
                  <p class="period-name">{periodTypeLabel(period)}{#if period.name && period.period_type !== 'custom'}<span class="muted period-subname">· {period.name}</span>{/if}</p>
                  <p class="muted">{formatExpiry(period)} · 周期 {period.cycle}</p>
                  <div class="badges"><ExpiryBadge {period} /></div>
                </div>
                <button type="button" class="toggle" aria-expanded={Boolean(expanded[period.id])} onclick={() => toggle(period)}>{expanded[period.id] ? '收起' : '详情'}</button>
              </div>
              <div class="actions wrap">
                {#if period.status !== 'cancelled'}
                  <button type="button" class="primary" onclick={() => navigate(`/app/periods/${period.id}/renew`)} disabled={!canWrite() || actionBusy}>续期</button>
                  <button type="button" onclick={() => navigate(`/app/periods/${period.id}/rules`)} disabled={!canWrite() || actionBusy}>提醒规则</button>
                  <button type="button" onclick={() => navigate(`/app/periods/${period.id}/edit`)} disabled={!canWrite() || actionBusy}>编辑有效期</button>
                {:else}
                  <p class="muted">已取消的有效期不可恢复，也不能再编辑或续期。</p>
                  <button type="button" class="danger" onclick={() => askDeletePeriod(period)} disabled={!canWrite() || actionBusy || deleteConflict.changed || deleteConflict.open}>{labels.deletePeriod}</button>
                {/if}
              </div>
              {#if expanded[period.id]}
                <div class="section stack">
                  <h3>提醒规则</h3>
                  {#if sections[period.id]?.rulesLoading && !sections[period.id]?.rules}<p role="status">正在加载规则…</p>
                  {:else if sections[period.id]?.rules?.data.length}
                    <ul class="plain">
                      {#each sections[period.id]!.rules!.data as rule (rule.id)}
                        <li>{rule.days_before} 天前 {rule.reminder_time} · {rule.enabled ? '启用' : '已禁用'}</li>
                      {/each}
                    </ul>
                    {#if sections[period.id]?.rules?.next_cursor}
                      <button type="button" onclick={() => loadRules(period.id, true)} disabled={sections[period.id]?.rulesLoading}>加载更多规则</button>
                    {/if}
                  {:else if !sections[period.id]?.rulesError}<p class="muted">暂无提醒规则。</p>{/if}
                  {#if sections[period.id]?.rulesError}<ErrorBanner message={errorMessage(sections[period.id]?.rulesError)} onretry={() => loadRules(period.id, Boolean(sections[period.id]?.rules))} />{/if}
                  <h3>续期历史</h3>
                  {#if sections[period.id]?.renewalsLoading && !sections[period.id]?.renewals}<p role="status">正在加载续期历史…</p>
                  {:else if sections[period.id]?.renewals?.data.length}
                    <ul class="plain">
                      {#each sections[period.id]!.renewals!.data as renewal (renewal.id)}
                        <li>
                          <p>{renewalDate(renewal, 'previous')} → {renewalDate(renewal, 'new')}</p>
                          <p class="muted">周期 {renewal.previous_cycle} · {formatMoney(renewal.amount, renewal.currency)} · {formatDateTime(renewal.renewed_at, renewal.timezone)}</p>
                          {#if renewal.notes}<p class="preserve">{renewal.notes}</p>{/if}
                        </li>
                      {/each}
                    </ul>
                    {#if sections[period.id]?.renewals?.next_cursor}
                      <button type="button" onclick={() => loadRenewals(period.id, true)} disabled={sections[period.id]?.renewalsLoading}>加载更多历史</button>
                    {/if}
                  {:else if !sections[period.id]?.renewalsError}<p class="muted">暂无续期记录。</p>{/if}
                  {#if sections[period.id]?.renewalsError}<ErrorBanner message={errorMessage(sections[period.id]?.renewalsError)} onretry={() => loadRenewals(period.id, Boolean(sections[period.id]?.renewals))} />{/if}
                  {#if period.status !== 'cancelled'}
                    <div class="actions"><button type="button" class="danger" onclick={() => askDeletePeriod(period)} disabled={!canWrite() || actionBusy || deleteConflict.changed || deleteConflict.open}>{labels.deletePeriod}</button></div>
                  {/if}
                </div>
              {/if}
            </li>
          {/each}
        </ul>
      </AsyncState>
      {#if moreError}<ErrorBanner message={errorMessage(moreError)} onretry={loadMore} />{/if}
      {#if nextCursor !== null}
        <button type="button" onclick={loadMore} disabled={loadingMore}>{loadingMore ? '正在加载…' : '加载更多有效期'}</button>
      {/if}
    </div>

    <div class="section stack">
      <SectionHeader title="管理卡片">{#snippet children()}{/snippet}</SectionHeader>
      <div class="actions">
        {#if sim.status === 'active'}
          <button type="button" onclick={() => { archiveOpen = true; }} disabled={!canWrite() || actionBusy}>归档</button>
        {:else}
          <button type="button" onclick={() => { restoreOpen = true; }} disabled={!canWrite() || actionBusy}>恢复</button>
        {/if}
        <button type="button" class="danger" onclick={() => { actionError = ''; deleteSimOpen = true; }} disabled={!canWrite() || actionBusy}>{labels.deleteCard}</button>
      </div>
    </div>
  {:else}
    <ErrorBanner message="卡片不存在或已不可访问。" onretry={refresh} />
  {/if}
</section>

<ConfirmDialog bind:open={archiveOpen} title="归档这张卡片？" message="归档后卡片不再计入首页统计，未发送的提醒会被取消。资料与历史仍保留，可随时恢复。" confirmLabel="确认归档" busy={actionBusy} onconfirm={archive} oncancel={() => { archiveOpen = false; }} />
<ConfirmDialog bind:open={restoreOpen} title="恢复这张卡片？" message="恢复会取消未发送提醒，未取消有效期的周期会加一，旧的版本将失效；需要按最新数据重新编辑或续期。" confirmLabel="确认恢复" busy={actionBusy} onconfirm={restore} oncancel={() => { restoreOpen = false; }} />
<ConfirmDialog bind:open={deleteSimOpen} title={labels.deleteCardTitle} message={labels.deleteCardMessage(sim?.name ?? '')} confirmLabel={labels.deleteCardConfirm} busy={actionBusy} onconfirm={deleteSim} oncancel={() => { deleteSimOpen = false; }} />
<ConfirmDialog bind:open={deletePeriodOpen} title={labels.deletePeriodTitle} message={labels.deletePeriodMessage(deleteTarget ? deleteTarget.name || periodTypeLabel(deleteTarget) : '')} confirmLabel={labels.deletePeriodConfirm} busy={actionBusy} onconfirm={deletePeriod} oncancel={() => { deletePeriodOpen = false; deleteTarget = null; }} />
<ConfirmDialog bind:open={deleteConflict.open} title="有效期周期已变化" message={labels.deleteConflict} confirmLabel="使用最新值" busy={deleteConflict.loading} onconfirm={useLatestPeriod} oncancel={() => deleteConflict.dismiss()}>
  {#if deleteConflict.loading}<p role="status">正在获取最新有效期…</p>
  {:else if deleteConflict.failed}<ErrorBanner message={labels.deleteConflictFailed} onretry={() => { void deleteConflict.retry(); }} />
  {:else if deleteConflict.current}<p>服务端名称：{deleteConflict.current.name || periodTypeLabel(deleteConflict.current)}</p><p>周期：{conflictPreviousCycle} → {deleteConflict.current.cycle}</p><p>截止：{formatExpiry(deleteConflict.current)}</p><p>状态：{periodStatusLabels[deleteConflict.current.status]}</p>{/if}
</ConfirmDialog>
{#if deleteConflict.changed && !deleteConflict.open}<ErrorBanner message={deleteConflict.failed ? labels.deleteConflictFailed : labels.deleteConflict} onretry={() => { void deleteConflict.retry(); }} />{/if}

<style>
  .heading{display:flex;align-items:center;gap:10px;min-width:0}
  .heading-text{flex:1;min-width:0;overflow-wrap:anywhere}
  h1{font-size:18px;line-height:1.4}
  .muted{color:var(--text-secondary);margin:0;overflow-wrap:anywhere}
  .profile{display:grid;gap:6px;margin:0}
  .profile div{display:flex;gap:10px;min-width:0}
  dt{flex:0 0 72px;color:var(--text-secondary)}
  dd{margin:0;min-width:0;overflow-wrap:anywhere}
  .preserve{white-space:pre-wrap}
  .periods,.plain{list-style:none;margin:0;padding:0;display:grid;gap:12px;min-width:0}
  .period{padding:14px 0;border-top:1px solid var(--border);min-width:0;display:grid;gap:12px}
  .period-head{display:flex;align-items:flex-start;gap:10px;min-width:0}
  .period-text{flex:1;min-width:0;display:grid;gap:4px}
  .period-name{margin:0;font-weight:600;overflow-wrap:anywhere}
  .period-subname{margin-left:4px;font-weight:400}
  .badges{display:flex;align-items:center;gap:6px;flex-wrap:wrap}
  h3{font-size:13px;margin:0}
  .wrap{flex-wrap:wrap}
  .danger{color:var(--badge-danger-fg)}
</style>
