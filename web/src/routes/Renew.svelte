<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { api } from '../lib/api/resources';
  import { ApiError } from '../lib/api/errors';
  import type { DatePrecision, Period, Renewal, RenewalCreateInput } from '../lib/api/types';
  import { ConflictFlow } from '../lib/conflict.svelte';
  import { Submitter } from '../lib/form.svelte';
  import { acceptMe, canWrite, onSessionFailure, session } from '../lib/session.svelte';
  import { back } from '../lib/router';
  import { formatDateTime, isValidDate, localDateAt, wallTimeToRFC3339, type Occurrence } from '../lib/domain/datetime';
  import { formatExpiry } from '../lib/domain/expiry';
  import { formatMoney, parseMoney } from '../lib/domain/money';
  import { formatTermsCycle } from '../lib/domain/terms';
  import { errorMessage, labels, periodTypeLabel, renewalLabels as text } from '../lib/domain/labels';
  import AsyncState from '../components/AsyncState.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import DatePrecisionInput from '../components/DatePrecisionInput.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import FormField from '../components/FormField.svelte';
  import MoneyInput from '../components/MoneyInput.svelte';
  import SectionHeader from '../components/SectionHeader.svelte';
  import Toast from '../components/Toast.svelte';

  let { id }: { id: string } = $props();
  let period = $state<Period | null>(null);
  let precision = $state<DatePrecision>('date');
  let expires = $state('');
  let occurrence = $state<Occurrence | undefined>();
  let amount = $state('');
  let currency = $state('');
  let notes = $state('');
  let dirty = $state(false);
  let loading = $state(true);
  let periodError = $state('');
  let fieldError = $state('');
  let notesError = $state('');
  let history = $state<Renewal[]>([]);
  let historyCursor = $state<string | null>(null);
  let historyLoading = $state(false);
  let historyMore = $state(false);
  let historyError = $state('');
  let historyRetryMore = $state(false);
  let historyReady = $state(false);
  let toast = $state('');
  let replayMessage = $state('');
  let reviewRequired = $state(false);
  let refreshRequired = $state(false);
  let accountLoading = $state(false);
  let controller: AbortController | undefined;
  let sequence = 0;
  let historySequence = 0;
  const submitter = new Submitter();
  const conflict = new ConflictFlow<Period>(() => api.period(id, controller?.signal));
  const terminal = $derived(session.auth === 'expired' || session.auth === 'forbidden');
  const writable = $derived(canWrite() && period !== null && period.status !== 'cancelled' && !loading && !refreshRequired && !reviewRequired && !conflict.open && !submitter.busy);

  function current(request: number, signal: AbortSignal): boolean {
    return request === sequence && !signal.aborted && !terminal;
  }

  async function readPeriod(): Promise<void> {
    if (!controller || terminal) return;
    const request = sequence;
    const signal = controller.signal;
    loading = true;
    periodError = '';
    try {
      const result = await api.period(id, signal);
      if (!current(request, signal)) return;
      period = result;
      refreshRequired = false;
    } catch (error) {
      if (current(request, signal)) periodError = errorMessage(error);
    } finally {
      if (current(request, signal)) loading = false;
    }
  }

  async function readHistory(more = false): Promise<void> {
    if (!controller || terminal || (more && (!historyCursor || historyLoading || historyMore))) return;
    const request = sequence;
    const batch = ++historySequence;
    const signal = controller.signal;
    const after = more ? historyCursor : null;
    if (more) historyMore = true;
    else historyLoading = true;
    historyError = '';
    historyRetryMore = more;
    try {
      const result = await api.renewals(id, after, signal);
      if (!current(request, signal) || batch !== historySequence) return;
      const known = new Set(more ? history.map((entry) => entry.id) : []);
      history = [...(more ? history : []), ...result.data.filter((entry) => !known.has(entry.id))];
      historyCursor = result.next_cursor;
      historyReady = true;
    } catch (error) {
      if (current(request, signal) && batch === historySequence) historyError = errorMessage(error);
    } finally {
      if (current(request, signal) && batch === historySequence) historyLoading = historyMore = false;
    }
  }

  $effect(() => {
    const periodID = id;
    untrack(() => {
      controller?.abort();
      controller = new AbortController();
      ++sequence;
      ++historySequence;
      period = null;
      history = [];
      historyCursor = null;
      historyReady = false;
      historyError = '';
      historyLoading = historyMore = false;
      expires = amount = currency = notes = '';
      occurrence = undefined;
      dirty = false;
      fieldError = notesError = toast = replayMessage = '';
      refreshRequired = reviewRequired = false;
      conflict.dismiss();
      submitter.clear();
      const signal = controller.signal;
      accountLoading = session.me === null && !terminal;
      if (accountLoading) {
        void api.me(signal)
          .then((user) => { if (!signal.aborted) acceptMe(user); })
          .catch(() => {})
          .finally(() => { if (!signal.aborted) accountLoading = false; });
      }
      void readPeriod().then(() => {
        if (period?.id !== periodID || terminal) return;
        precision = period.date_precision;
        void readHistory();
      });
    });
    return () => { ++sequence; controller?.abort(); };
  });

  $effect(() => { session.dirty = dirty; });
  const stopFailure = onSessionFailure(() => {
    ++sequence;
    controller?.abort();
    loading = historyLoading = historyMore = accountLoading = false;
  });
  onDestroy(() => { stopFailure(); session.dirty = false; });

  function draftBody(source: Period): RenewalCreateInput {
    fieldError = notesError = '';
    let expiration = expires;
    try {
      if (precision === 'date') {
        if (!isValidDate(expires)) throw new Error(text.invalidDate);
        // Calendar-date feedback only: the API owns the exclusive date cutoff.
        const oldDate = source.local_expires_date ?? localDateAt(source.expires_at, source.timezone);
        const today = localDateAt(new Date().toISOString(), source.timezone);
        if (expires < oldDate || (source.date_precision === 'date' && expires === oldDate) || expires < today) {
          throw new Error(text.laterExpiry);
        }
      } else {
        expiration = wallTimeToRFC3339(expires, source.timezone, occurrence);
        if (Date.parse(expiration) <= Date.parse(source.expires_at) || Date.parse(expiration) <= Date.now()) {
          throw new Error(text.laterExpiry);
        }
      }
    } catch (error) {
      fieldError = error instanceof Error ? error.message : text.invalidDate;
      throw error;
    }
    if (Array.from(notes).length > 4000) {
      notesError = text.notesTooLong;
      throw new Error(notesError);
    }
    return {
      expected_cycle: source.cycle,
      date_precision: precision,
      expires_at: expiration,
      ...parseMoney(amount, currency),
      ...(notes ? { notes } : {}),
    };
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!writable || !period) return;
    const source = period;
    const request = sequence;
    const signal = controller?.signal;
    toast = replayMessage = '';
    await submitter.run(async () => {
      const body = draftBody(source);
      try {
        const result = await api.renew(id, body, signal);
        if (request !== sequence || signal?.aborted || terminal) return;
        if (result.status === 201) toast = text.success;
        else if (result.status === 200) replayMessage = text.replayed;
        dirty = false;
        expires = amount = currency = notes = '';
        occurrence = undefined;
        refreshRequired = true;
        await Promise.all([readPeriod(), readHistory()]);
      } catch (error) {
        if (request !== sequence || signal?.aborted || terminal) return;
        if (error instanceof ApiError && error.kind === 'conflict') {
          reviewRequired = true;
          await conflict.present();
          return;
        }
        throw error;
      }
    });
  }

  function acceptLatest(): void {
    if (terminal) return;
    if (conflict.loading) return;
    if (conflict.failed || !conflict.current) { void conflict.retry(); return; }
    period = conflict.current;
    if (period.status === 'cancelled') {
      conflict.dismiss();
      back(`/app/services/${period.service_id}`);
      return;
    }
    reviewRequired = false;
    refreshRequired = false;
    conflict.dismiss();
    submitter.clear();
    fieldError = '';
  }

</script>

<div class="content stack renew-page">
  <section class="section stack">
    <h1>{text.title}</h1>
    {#if !canWrite() && !accountLoading}<p class="notice">{labels.readOnly}</p>{/if}
    {#if !period}
      <AsyncState {loading} error={periodError} onretry={terminal ? undefined : () => { void readPeriod().then(() => { if (period) { precision = period.date_precision; void readHistory(); } }); }} />
    {:else}
      <h2>{period.name || periodTypeLabel(period)}</h2>
      <dl class="renew-facts">
        <dt>{text.previousExpiry}</dt><dd>{formatExpiry(period)}</dd>
        <dt>{text.terms}</dt><dd>{formatTermsCycle(period.terms)} · {formatMoney(period.terms.amount, period.terms.currency)}</dd>
        <dt>{text.expiryInstant}</dt><dd><time datetime={period.expires_at}>{formatDateTime(period.expires_at, period.timezone)}</time></dd>
        <dt>{text.currentCycle}</dt><dd>{period.cycle}</dd>
        <dt>{text.timezone}</dt><dd>{period.timezone}</dd>
      </dl>
      <p class="muted">{text.termsReferenceHint}</p>
      {#if periodError}<ErrorBanner message={periodError} onretry={terminal ? undefined : () => { void readPeriod(); }} />{/if}
      <Toast message={toast} ondismiss={() => { toast = ''; }} />
      {#if replayMessage}<p role="status" class="notice">{replayMessage}</p>{/if}
      {#if period.status === 'cancelled'}
        <p class="notice">{text.cancelledPeriodHint}</p>
      {:else}
        {#if reviewRequired && !conflict.open}
          <ErrorBanner message={text.conflictPending} onretry={terminal ? undefined : () => { void conflict.present(); }} />
        {/if}
        <form class="stack" onsubmit={submit} oninput={() => { dirty = true; }}>
          <DatePrecisionInput id="renew-expires" bind:precision bind:expires bind:occurrence zone={period.timezone} disabled={!writable} error={fieldError} />
          <p class="muted">{text.expiryHint}</p>
          <p class="muted">{text.actualPaymentHint}</p>
          <MoneyInput id="renew-amount" bind:amount bind:currency disabled={!writable} />
          <FormField id="renew-notes" label={text.notes} error={notesError} hint={text.notesHint}>
            {#snippet action()}
              {#if notes}<button type="button" class="text-button" aria-label={text.clearNotes} disabled={!writable} onclick={() => { notes = ''; dirty = true; }}>清除</button>{/if}
            {/snippet}
            {#snippet children(field)}
              <textarea id={field.id} bind:value={notes} disabled={!writable} rows="4" aria-describedby={field.describedby} aria-invalid={field.invalid}></textarea>
            {/snippet}
          </FormField>
          {#if submitter.error && !fieldError && !notesError}<ErrorBanner message={submitter.error instanceof ApiError ? errorMessage(submitter.error) : text.invalidForm} />{/if}
          <button class="primary" type="submit" aria-label={text.submit} disabled={!writable}>{submitter.busy ? text.submitting : text.submit}</button>
        </form>
      {/if}
    {/if}
  </section>
  {#if period}
    <section class="section stack" aria-label={text.history}>
      <SectionHeader title={text.history}>{#snippet children()}{/snippet}</SectionHeader>
      {#if historyError}<ErrorBanner message={historyError} onretry={terminal ? undefined : () => { void readHistory(historyRetryMore); }} />{/if}
      {#if historyLoading}<p role="status">{labels.loading}</p>{/if}
      {#if historyReady && history.length === 0}<p class="notice">{text.noHistory}</p>{/if}
      <ul class="renew-history">
        {#each history as entry (entry.id)}
          <li class="stack">
            <p>{text.renewedAt} <time datetime={entry.renewed_at}>{formatDateTime(entry.renewed_at, entry.timezone)}</time></p>
            <p>{text.previousCycle} {entry.previous_cycle} · {text.newExpiry} {entry.new_date_precision === 'date' ? entry.new_local_expires_date ?? text.dateUnavailable : formatDateTime(entry.new_expires_at, entry.timezone)}</p>
            <p>{formatMoney(entry.amount, entry.currency)}</p>
            {#if entry.notes}<p class="renew-notes">{entry.notes}</p>{/if}
          </li>
        {/each}
      </ul>
      {#if historyCursor}<button type="button" disabled={terminal || historyLoading || historyMore} onclick={() => { void readHistory(true); }}>{historyMore ? labels.loading : labels.loadMore}</button>{/if}
    </section>
  {/if}
</div>

<ConfirmDialog open={conflict.open} title={text.conflictTitle} message={text.conflictMessage} confirmLabel={conflict.failed ? text.reloadLatest : conflict.current?.status === 'cancelled' ? labels.back : text.editLatest} cancelLabel={labels.back} busy={conflict.loading} onconfirm={acceptLatest} oncancel={() => conflict.dismiss()}>
  {#if conflict.loading}<p role="status">{labels.loading}</p>
  {:else if conflict.failed}<p role="alert">{text.conflictReloadFailed}</p>
  {:else if conflict.current}
    <dl class="renew-facts"><dt>{text.cycle}</dt><dd>{period?.cycle} → {conflict.current.cycle}</dd><dt>{text.currentExpiry}</dt><dd>{formatExpiry(conflict.current)}</dd><dt>{text.expiryInstant}</dt><dd>{formatDateTime(conflict.current.expires_at, conflict.current.timezone)}</dd></dl>
    {#if conflict.current.status === 'cancelled'}<p>{text.cancelledPeriodHint}</p>{/if}
  {/if}
</ConfirmDialog>

<style>
  h2{font-size:16px;margin:0}p{margin:0;overflow-wrap:anywhere}.renew-facts{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,2fr);gap:8px;margin:0}.renew-facts dt{color:var(--text-secondary)}.renew-facts dd{margin:0;overflow-wrap:anywhere}.renew-history{list-style:none;margin:0;padding:0}.renew-history li{padding:12px 0;border-bottom:1px solid var(--border)}.renew-history li:last-child{border-bottom:0}.renew-notes{white-space:pre-wrap}
</style>
