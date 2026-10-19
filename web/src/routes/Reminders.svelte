<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api } from '../lib/api/resources';
  import { ApiError } from '../lib/api/errors';
  import type { ID, ReminderJob, ReminderStatusFilter } from '../lib/api/types';
  import { acceptMe, canWrite, onSessionFailure, session } from '../lib/session.svelte';
  import { formatDateTime } from '../lib/domain/datetime';
  import { errorMessage, labels, remindersLabels as text, reminderStatusLabels } from '../lib/domain/labels';
  import { getInitData } from '../lib/telegram/sdk';
  import AsyncState from '../components/AsyncState.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import SectionHeader from '../components/SectionHeader.svelte';
  import Toast from '../components/Toast.svelte';

  let { periodNames = {} }: { periodNames?: Readonly<Record<ID, string>> } = $props();
  type Detail = { open: boolean; loading: boolean; error: string; data?: ReminderJob };
  const statuses: ReminderStatusFilter[] = ['all', 'pending', 'sending', 'sent', 'failed', 'cancelled'];
  let status = $state<ReminderStatusFilter>('all');
  let jobs = $state<ReminderJob[]>([]);
  let nextCursor = $state<ID | null>(null);
  let loading = $state(true);
  let loadingMore = $state(false);
  let refreshing = $state(false);
  let listError = $state('');
  let moreError = $state('');
  let failedMode = $state<'more' | 'refresh'>('more');
  let details = $state<Record<ID, Detail>>({});
  let cancelTarget = $state<ReminderJob | null>(null);
  let confirmOpen = $state(false);
  let cancelling = $state(false);
  let actionError = $state('');
  let toast = $state('');
  let listRequest = 0;
  let listController: AbortController | undefined;
  let accountController: AbortController | undefined;
  let cancelController: AbortController | undefined;
  const detailControllers = new Map<ID, AbortController>();
  let alive = true;
  const timezone = $derived(session.me?.timezone ?? 'UTC');
  const readable = () => session.auth !== 'expired' && session.auth !== 'forbidden' && getInitData().length > 0;
  const cancellable = (job: ReminderJob) => ['pending', 'sending', 'failed'].includes(job.status);
  const time = (value: string) => formatDateTime(value, timezone);

  // Only allowlisted categories are displayed; backend error text may contain URLs or credentials.
  function failureCategory(code: string | null): string {
    switch (code) {
      case 'network': return text.networkError;
      case 'rate_limited': return text.rateLimitError;
      case 'remote_rejected': return text.permissionError;
      case 'no_private_chat': return text.noPrivateChatError;
      case 'remote_unavailable': return text.deliveryError;
      default: return text.unknownError;
    }
  }

  function abortDetails() {
    for (const controller of detailControllers.values()) controller.abort();
    detailControllers.clear();
  }

  async function load(mode: 'reset' | 'more' | 'refresh' = 'reset') {
    if (!readable()) { loading = false; return; }
    if (mode === 'more' && (loadingMore || loading || refreshing || nextCursor === null)) return;
    listController?.abort();
    const controller = new AbortController();
    listController = controller;
    const request = ++listRequest;
    const filter = status;
    const after = mode === 'more' ? nextCursor : null;
    listError = '';
    moreError = '';
    failedMode = mode === 'more' ? 'more' : 'refresh';
    loading = mode === 'reset';
    loadingMore = mode === 'more';
    refreshing = mode === 'refresh';
    if (mode === 'reset') {
      jobs = [];
      nextCursor = null;
      abortDetails();
      details = {};
    }
    try {
      const page = await api.reminders(after, filter, controller.signal);
      if (!alive || controller.signal.aborted || request !== listRequest || filter !== status) return;
      jobs = mode === 'more' ? [...jobs, ...page.data.filter((job) => !jobs.some((old) => old.id === job.id))] : page.data;
      nextCursor = page.next_cursor;
    } catch (error) {
      if (!alive || controller.signal.aborted || request !== listRequest) return;
      if (jobs.length > 0) moreError = errorMessage(error);
      else listError = errorMessage(error);
    } finally {
      if (alive && request === listRequest) {
        loading = false;
        loadingMore = false;
        refreshing = false;
      }
    }
  }

  function changeStatus(event: Event) {
    status = (event.currentTarget as HTMLSelectElement).value as ReminderStatusFilter;
    actionError = '';
    toast = '';
    confirmOpen = false;
    cancelTarget = null;
    void load();
  }

  /** `keep` retains a server-refreshed row even when the active filter changed. */
  function replaceJob(job: ReminderJob, keep = false) {
    jobs = jobs.flatMap((old) => old.id !== job.id
      ? [old]
      : keep || status === 'all' || status === job.status ? [job] : []);
    const detail = details[job.id];
    if (detail) details[job.id] = { ...detail, open: true, loading: false, error: '', data: job };
  }

  async function loadDetail(id: ID) {
    if (!readable()) return;
    detailControllers.get(id)?.abort();
    const controller = new AbortController();
    detailControllers.set(id, controller);
    details[id] = { ...details[id], open: true, loading: true, error: '' };
    try {
      const job = await api.reminder(id, controller.signal);
      if (!alive || controller.signal.aborted || detailControllers.get(id) !== controller) return;
      replaceJob(job);
      details[id] = { open: true, loading: false, error: '', data: job };
    } catch (error) {
      if (!alive || controller.signal.aborted || detailControllers.get(id) !== controller) return;
      details[id] = { ...details[id], open: true, loading: false, error: errorMessage(error) };
    } finally {
      if (detailControllers.get(id) === controller) detailControllers.delete(id);
    }
  }

  function toggleDetail(id: ID) {
    if (details[id]?.open) {
      detailControllers.get(id)?.abort();
      detailControllers.delete(id);
      details[id] = { ...details[id], open: false, loading: false };
    } else void loadDetail(id);
  }

  function askCancel(job: ReminderJob) {
    if (!canWrite() || cancelling || !cancellable(job)) return;
    actionError = '';
    cancelTarget = job;
    confirmOpen = true;
  }

  async function cancelReminder() {
    const target = cancelTarget;
    if (!target || cancelling || !canWrite() || !cancellable(target)) return;
    cancelling = true;
    actionError = '';
    const controller = new AbortController();
    cancelController = controller;
    // No pre-cancellation list/detail response may overwrite the resulting state.
    listController?.abort();
    ++listRequest;
    loading = loadingMore = refreshing = false;
    detailControllers.get(target.id)?.abort();
    detailControllers.delete(target.id);
    try {
      await api.cancelReminder(target.id, controller.signal);
      if (!alive || controller.signal.aborted) return;
      replaceJob({ ...target, status: 'cancelled' });
      toast = text.cancelled;
      confirmOpen = false;
      cancelTarget = null;
      await load('refresh');
    } catch (error) {
      if (!alive || controller.signal.aborted) return;
      if (error instanceof ApiError && error.status === 409) {
        confirmOpen = false;
        cancelTarget = null;
        try {
          const current = await api.reminder(target.id, controller.signal);
          if (!alive || controller.signal.aborted) return;
          replaceJob(current, true);
          toast = text.changed(reminderStatusLabels[current.status]);
        } catch (refreshError) {
          actionError = text.conflictRefreshFailed;
          details[target.id] = { open: true, loading: false, error: errorMessage(refreshError) };
        }
      } else {
        confirmOpen = false;
        actionError = errorMessage(error);
      }
    } finally {
      if (alive) cancelling = false;
    }
  }

  async function loadAccount() {
    if (session.me !== null || !readable()) return;
    accountController?.abort();
    const controller = new AbortController();
    accountController = controller;
    try {
      const user = await api.me(controller.signal);
      if (alive && !controller.signal.aborted) acceptMe(user);
    } catch {
      // api.me failures are already reflected in the global session state.
    }
  }

  onMount(() => {
    void loadAccount();
    void load();
    return onSessionFailure(() => {
      listController?.abort();
      accountController?.abort();
      ++listRequest;
      abortDetails();
      cancelController?.abort();
      listError = actionError = '';
      moreError = '';
      cancelTarget = null;
      details = {};
      loading = loadingMore = refreshing = cancelling = confirmOpen = false;
    });
  });
  onDestroy(() => {
    alive = false;
    listController?.abort();
    accountController?.abort();
    abortDetails();
    cancelController?.abort();
  });
</script>

<section class="content stack" aria-label={text.title}>
  <SectionHeader title={text.title}>
    <button type="button" onclick={() => load('refresh')} disabled={!readable() || loading || loadingMore || refreshing || cancelling}>{labels.refresh}</button>
  </SectionHeader>
  <label class="filter">{text.filter}
    <select value={status} onchange={changeStatus} disabled={!readable() || cancelling}>
      {#each statuses as option}<option value={option}>{option === 'all' ? '全部状态' : reminderStatusLabels[option]}</option>{/each}
    </select>
  </label>
  <p class="muted">{text.zone}：{timezone}</p>
  {#if !canWrite()}<p class="notice">{labels.readOnly}</p>{/if}
  <Toast message={toast} ondismiss={() => { toast = ''; }} />
  {#if actionError}<ErrorBanner message={actionError} />{/if}
  {#if refreshing}<p role="status">{text.refreshing}</p>{/if}
  <AsyncState {loading} error={listError} empty={readable() && jobs.length === 0} emptyMessage={text.empty} onretry={readable() ? () => load() : undefined}>
    <ul class="reminders">
      {#each jobs as job (job.id)}
        <li class="job stack" aria-label={text.task(job.id)}>
          <div class="job-heading"><h3>{periodNames[job.validity_period_id] || text.period(job.validity_period_id)}</h3><span class="badge" data-status={job.status}>{reminderStatusLabels[job.status]}</span></div>
          <dl>
            <div><dt>{text.scheduled}</dt><dd>{time(job.scheduled_at)}</dd></div>
            {#if job.sent_at}<div><dt>{text.sent}</dt><dd>{time(job.sent_at)}</dd></div>{/if}
            <div><dt>{text.attempts}</dt><dd>{job.attempts}</dd></div>
            {#if job.status === 'failed'}<div><dt>{text.errorCategory}</dt><dd>{failureCategory(job.last_error_code)}</dd></div>{/if}
          </dl>
          <div class="actions">
            <button type="button" aria-expanded={details[job.id]?.open ?? false} aria-controls={`reminder-${job.id}`} onclick={() => toggleDetail(job.id)} disabled={!readable() || cancelling}>{details[job.id]?.open ? text.hideDetail : text.detail}</button>
            {#if cancellable(job)}<button type="button" onclick={() => askCancel(job)} disabled={!canWrite() || cancelling}>{text.cancel}</button>{/if}
          </div>
          {#if details[job.id]?.open}
            {@const detail = details[job.id]}
            {#if detail}
              <div id={`reminder-${job.id}`} class="detail stack">
                <AsyncState loading={detail.loading} error={detail.error} onretry={readable() ? () => loadDetail(job.id) : undefined}>
                  {#if detail.data}
                    <dl>
                      <div><dt>{text.task(job.id)}</dt><dd>{reminderStatusLabels[detail.data.status]}</dd></div>
                      <div><dt>{text.rule}</dt><dd>#{detail.data.reminder_rule_id}</dd></div>
                      <div><dt>{text.cycle}</dt><dd>{detail.data.cycle}</dd></div>
                      <div><dt>{text.revision}</dt><dd>{detail.data.rule_revision}</dd></div>
                      <div><dt>{text.snapshot}</dt><dd>{time(detail.data.expires_at_snapshot)}</dd></div>
                      {#if detail.data.next_attempt_at}<div><dt>{text.nextAttempt}</dt><dd>{time(detail.data.next_attempt_at)}</dd></div>{/if}
                      <div><dt>{text.created}</dt><dd>{time(detail.data.created_at)}</dd></div>
                      <div><dt>{text.updated}</dt><dd>{time(detail.data.updated_at)}</dd></div>
                    </dl>
                  {/if}
                </AsyncState>
              </div>
            {/if}
          {/if}
        </li>
      {/each}
    </ul>
  </AsyncState>
  {#if moreError}<ErrorBanner message={`${failedMode === 'more' ? text.partial : text.refreshFailed} ${moreError}`} onretry={readable() ? () => load(failedMode) : undefined} />{/if}
  {#if nextCursor && !listError}<button type="button" onclick={() => load('more')} disabled={!readable() || loadingMore || refreshing || cancelling}>{loadingMore ? labels.loading : labels.loadMore}</button>{/if}
</section>

<ConfirmDialog bind:open={confirmOpen} title={text.cancelTitle} message={cancelTarget?.status === 'sending' ? text.sendingWarning : text.cancelMessage} confirmLabel={text.cancelConfirm} busy={cancelling} onconfirm={cancelReminder} oncancel={() => { cancelTarget = null; }} />

<style>
  .filter{display:grid;gap:6px}.filter select{width:100%}.muted{color:var(--text-secondary);margin:0}.reminders{list-style:none;padding:0;margin:0}.job{padding:16px 0;border-bottom:1px solid var(--border)}.job:first-child{padding-top:0}.job-heading{display:flex;align-items:start;justify-content:space-between;gap:8px}h3{margin:0;font-size:15px;overflow-wrap:anywhere;min-width:0}.badge{flex-shrink:0;border-radius:6px;padding:3px 7px;background:var(--page-bg);color:var(--text-secondary);font-size:12px}.badge[data-status="failed"]{color:var(--badge-danger-fg);background:var(--badge-danger-bg)}.badge[data-status="sent"]{color:var(--badge-ok-fg);background:var(--badge-ok-bg)}dl{margin:0;display:grid;gap:7px}dl div{display:flex;justify-content:space-between;gap:12px;min-width:0}dt{color:var(--text-secondary)}dd{margin:0;text-align:right;overflow-wrap:anywhere;min-width:0}.detail{padding:12px;border:1px solid var(--border);border-radius:8px}.actions{flex-wrap:wrap}
</style>
