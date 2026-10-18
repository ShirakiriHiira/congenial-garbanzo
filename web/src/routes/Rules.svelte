<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { api } from '../lib/api/resources';
  import { ApiError } from '../lib/api/errors';
  import type { Period, Rule, RuleCreateInput } from '../lib/api/types';
  import { ConflictFlow } from '../lib/conflict.svelte';
  import { Submitter } from '../lib/form.svelte';
  import { acceptMe, canWrite, onSessionFailure, session } from '../lib/session.svelte';
  import { back } from '../lib/router';
  import { errorMessage, labels, periodTypeLabel, rulesLabels as copy } from '../lib/domain/labels';
  import FormField from '../components/FormField.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import Toast from '../components/Toast.svelte';

  let { id }: { id: string } = $props();
  const uid = $props.id();
  let period = $state<Period | null>(null);
  let periodLoading = $state(true);
  let periodError = $state<unknown>(null);
  let rules = $state<Rule[]>([]);
  let loaded = $state(false);
  let loading = $state(true);
  let loadingMore = $state(false);
  let listError = $state<unknown>(null);
  let nextCursor = $state<string | null>(null);
  let days = $state('0');
  let time = $state('09:00');
  let enabled = $state(true);
  let editing = $state<Rule | null>(null);
  let daysError = $state('');
  let timeError = $state('');
  let toast = $state('');
  let deleteTarget = $state<Rule | null>(null);
  let deleteOpen = $state(false);
  let reviewRequired = $state(false);
  let refreshRequired = $state(false);
  let conflictTarget = $state<Rule | null>(null);
  let accountLoading = $state(false);
  let generation = 0;
  let listRequest = 0;
  let periodRequest = 0;
  let listController: AbortController | undefined;
  let periodController: AbortController | undefined;
  let accountController: AbortController | undefined;
  const submitter = new Submitter();
  const conflict = new ConflictFlow<{ period: Period; rule: Rule | null }>(async () => {
    const currentGeneration = generation;
    const periodID = id;
    if (!await loadRules(false)) throw new Error('reload failed');
    const [currentPeriod, currentRule] = await Promise.all([
      api.period(periodID), conflictTarget ? api.rule(conflictTarget.id) : Promise.resolve(null),
    ]);
    if (currentGeneration !== generation || !mayRead()) throw new DOMException('Aborted', 'AbortError');
    period = currentPeriod;
    if (currentRule) rules = mergeRules(rules, [currentRule]);
    return { period: currentPeriod, rule: currentRule };
  });
  const writable = $derived(canWrite() && period !== null && period.status !== 'cancelled');
  const blocked = $derived(!writable || !loaded || loading || loadingMore || submitter.busy || conflict.open || reviewRequired || refreshRequired);
  const dirty = $derived(editing
    ? days !== String(editing.days_before) || time !== editing.reminder_time || enabled !== editing.enabled
    : days !== '0' || time !== '09:00' || enabled !== true);
  const duplicate = $derived(rules.find((rule) => rule.days_before === Number(days) && rule.reminder_time === time && rule.id !== editing?.id) ?? null);

  $effect(() => { session.dirty = dirty; });
  $effect(() => {
    const periodID = id;
    untrack(() => {
      generation += 1;
      period = null;
      rules = [];
      loaded = false;
      periodError = null;
      listError = null;
      reviewRequired = false;
      refreshRequired = false;
      resetDraft();
      loadAccount();
      void loadPeriod(periodID);
      void loadRules(false);
    });
    return () => {
      generation += 1;
      listController?.abort();
      periodController?.abort();
      accountController?.abort();
    };
  });
  const stopSession = onSessionFailure(() => {
    listController?.abort();
    periodController?.abort();
    accountController?.abort();
    accountLoading = false;
  });
  onDestroy(() => { stopSession(); session.dirty = false; });

  function mergeRules(previous: Rule[], incoming: Rule[]): Rule[] {
    const merged = new Map(previous.map((rule) => [rule.id, rule]));
    for (const rule of incoming) merged.set(rule.id, rule);
    return [...merged.values()].sort((a, b) => BigInt(a.id) < BigInt(b.id) ? -1 : BigInt(a.id) > BigInt(b.id) ? 1 : 0);
  }
  function mayRead() { return session.auth !== 'expired' && session.auth !== 'forbidden' && session.auth !== 'missing'; }
  function loadAccount() {
    accountController?.abort();
    accountLoading = session.me === null && mayRead();
    if (!accountLoading) return;
    const controller = new AbortController();
    accountController = controller;
    void api.me(controller.signal)
      .then((user) => { if (!controller.signal.aborted) acceptMe(user); })
      .catch(() => {})
      .finally(() => { if (!controller.signal.aborted) accountLoading = false; });
  }
  async function loadPeriod(periodID = id) {
    if (!mayRead()) { periodLoading = false; return; }
    const request = ++periodRequest;
    const currentGeneration = generation;
    periodController?.abort();
    const controller = new AbortController();
    periodController = controller;
    periodLoading = true;
    periodError = null;
    try {
      const result = await api.period(periodID, controller.signal);
      if (currentGeneration === generation && request === periodRequest && periodID === id && mayRead()) period = result;
    } catch (caught) {
      if (currentGeneration === generation && request === periodRequest && !controller.signal.aborted) periodError = caught;
    } finally {
      if (currentGeneration === generation && request === periodRequest) periodLoading = false;
    }
  }
  async function loadRules(more: boolean): Promise<boolean> {
    if (!mayRead()) { loading = false; loadingMore = false; return false; }
    if (more && (loading || loadingMore || nextCursor === null)) return false;
    const periodID = id;
    const request = ++listRequest;
    const currentGeneration = generation;
    listController?.abort();
    const controller = new AbortController();
    listController = controller;
    if (more) loadingMore = true;
    else { loading = true; loadingMore = false; }
    listError = null;
    try {
      const page = await api.rules(periodID, more ? nextCursor : null, controller.signal);
      if (currentGeneration !== generation || request !== listRequest || periodID !== id || !mayRead()) return false;
      rules = mergeRules(more ? rules : [], page.data);
      nextCursor = page.next_cursor;
      loaded = true;
      refreshRequired = false;
      return true;
    } catch (caught) {
      if (currentGeneration === generation && request === listRequest && !controller.signal.aborted) listError = caught;
      return false;
    } finally {
      if (currentGeneration === generation && request === listRequest) { loading = false; loadingMore = false; }
    }
  }
  function resetDraft() {
    editing = null;
    days = '0';
    time = '09:00';
    enabled = true;
    daysError = '';
    timeError = '';
    submitter.clear();
  }
  function beginEdit(rule: Rule) {
    if (blocked) return;
    if (dirty && !window.confirm(labels.leaveConfirm)) return;
    editing = rule;
    days = String(rule.days_before);
    time = rule.reminder_time;
    enabled = rule.enabled;
    daysError = '';
    timeError = '';
    submitter.clear();
  }
  function validate(): RuleCreateInput | null {
    const count = Number(days);
    daysError = /^\d+$/.test(days) && Number.isSafeInteger(count) && count >= 0 ? '' : copy.daysInvalid;
    timeError = /^([01]\d|2[0-3]):[0-5]\d$/.test(time) ? '' : copy.timeInvalid;
    return daysError || timeError ? null : { days_before: count, reminder_time: time, enabled };
  }
  async function mutate(task: () => Promise<void>, target: Rule | null) {
    if (blocked) return;
    const currentGeneration = generation;
    await submitter.run(async () => {
      try { await task(); }
      catch (caught) {
        if (currentGeneration !== generation) return;
        if (caught instanceof ApiError && caught.status === 409) {
          conflictTarget = target;
          reviewRequired = true;
          deleteOpen = false;
          await conflict.present();
          return;
        }
        throw caught;
      }
    });
  }
  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (blocked) return;
    const body = validate();
    if (!body) return;
    const currentEditing = editing;
    const currentGeneration = generation;
    await mutate(async () => {
      const rule = currentEditing
        ? await api.updateRule(currentEditing.id, { ...body, expected_revision: currentEditing.revision })
        : await api.createRule(id, body);
      if (currentGeneration !== generation || !mayRead()) return;
      rules = mergeRules(rules, [rule]);
      resetDraft();
      toast = currentEditing ? copy.updated : copy.created;
    }, currentEditing);
  }
  async function toggle(rule: Rule) {
    const currentGeneration = generation;
    await mutate(async () => {
      const updated = await api.updateRule(rule.id, { expected_revision: rule.revision, enabled: !rule.enabled });
      if (currentGeneration !== generation || !mayRead()) return;
      rules = mergeRules(rules, [updated]);
      if (editing?.id === updated.id) editing = updated;
      toast = copy.updated;
    }, rule);
  }
  function askDelete(rule: Rule) {
    if (blocked) return;
    deleteTarget = rule;
    deleteOpen = true;
  }
  async function softDelete() {
    if (!deleteTarget || !canWrite()) return;
    const rule = deleteTarget;
    const currentGeneration = generation;
    await mutate(async () => {
      await api.disableRule(rule.id, rule.revision);
      if (currentGeneration !== generation) return;
      deleteOpen = false;
      refreshRequired = true;
      if (editing?.id === rule.id) resetDraft();
      if (await loadRules(false)) toast = copy.deleted;
    }, rule);
  }
  function returnToCard() { back(period ? `/app/sims/${period.sim_id}` : '/app/'); }
  function acceptConflict() {
    if (conflict.loading) return;
    const current = conflict.current;
    if (conflict.failed || !current) { void conflict.retry(); return; }
    if (current.period.status === 'cancelled') { conflict.dismiss(); returnToCard(); return; }
    if (editing && current.rule && current.rule.id === editing.id) editing = current.rule;
    reviewRequired = false;
    conflict.dismiss();
    submitter.clear();
  }
</script>

<div class="content stack rules-page">
  <h1>{copy.title}</h1>
  {#if periodError}<ErrorBanner message={errorMessage(periodError)} onretry={mayRead() ? () => { void loadPeriod(); } : undefined} />{/if}
  {#if period}<p class="muted">{period.name || periodTypeLabel(period)} · {period.timezone}</p>{/if}
  {#if !canWrite() && !accountLoading}<p class="notice">{labels.readOnly}</p>{/if}
  {#if period?.status === 'cancelled'}<p class="notice">{copy.cancelled}</p>{/if}
  {#if toast}<Toast message={toast} ondismiss={() => { toast = ''; }} />{/if}
  {#if submitter.error}<ErrorBanner message={errorMessage(submitter.error)} />{/if}
  {#if reviewRequired && !conflict.open}
    <ErrorBanner message={copy.conflictDeferred} onretry={mayRead() ? () => { void conflict.present(); } : undefined} />
  {/if}
  {#if refreshRequired}<p class="notice">{copy.reloadRequired}</p>{/if}
  <section aria-label={copy.title} class="stack">
    {#if periodLoading || loading}<p role="status">{labels.loading}</p>{/if}
    {#if listError}
      {#if loaded}<p class="notice">{copy.partial}</p>{/if}
      <ErrorBanner message={errorMessage(listError)} onretry={mayRead() ? () => { void loadRules(false); } : undefined} />
    {:else if loaded && rules.length === 0 && !loading}<p>{copy.noRules}</p>{/if}
    {#each rules as rule (rule.id)}
      <article class:rules-page__disabled={!rule.enabled} class="rules-page__rule" aria-label={copy.summary(rule.days_before, rule.reminder_time)}>
        <div class="rules-page__summary"><h2>{copy.detail(rule.days_before, rule.reminder_time)}</h2><span class="rules-page__status">{rule.enabled ? copy.enabledState : copy.disabledState}</span></div>
        <p class="muted">{copy.revision} {rule.revision}</p>
        {#if period?.status !== 'cancelled'}
          <div class="actions">
            <button type="button" disabled={blocked} onclick={() => beginEdit(rule)}>{copy.edit}</button>
            <button type="button" disabled={blocked} onclick={() => { void toggle(rule); }}>{rule.enabled ? copy.disable : copy.enable}</button>
            <button type="button" disabled={blocked || !rule.enabled} onclick={() => askDelete(rule)}>{copy.remove}</button>
          </div>
        {/if}
      </article>
    {/each}
    {#if nextCursor !== null}<button type="button" disabled={loading || loadingMore || submitter.busy || reviewRequired || !mayRead()} onclick={() => { void loadRules(true); }}>{loadingMore ? labels.loading : labels.loadMore}</button>{/if}
  </section>
  {#if period && period.status !== 'cancelled'}
    <section class="section stack">
      <h2>{editing ? copy.edit : copy.create}</h2>
      <form onsubmit={save} novalidate>
        <FormField id={`${uid}-days`} label={copy.days} hint={copy.daysHint} error={daysError} required>
          {#snippet children(field)}<input id={field.id} type="text" inputmode="numeric" bind:value={days} aria-describedby={field.describedby} aria-invalid={field.invalid} disabled={submitter.busy} />{/snippet}
        </FormField>
        <FormField id={`${uid}-time`} label={copy.time} hint={copy.timeHint} error={timeError} required>
          {#snippet children(field)}<input id={field.id} type="time" step="60" bind:value={time} aria-describedby={field.describedby} aria-invalid={field.invalid} disabled={submitter.busy} />{/snippet}
        </FormField>
        <label class="rules-page__checkbox"><input type="checkbox" bind:checked={enabled} disabled={submitter.busy} />{copy.enabled}</label>
        <div class="actions"><button type="submit" class="primary" disabled={blocked}>{submitter.busy ? labels.loading : editing ? copy.save : copy.create}</button>{#if editing}<button type="button" disabled={submitter.busy || reviewRequired} onclick={resetDraft}>{copy.cancelEdit}</button>{/if}</div>
      </form>
    </section>
  {/if}
</div>

<ConfirmDialog bind:open={deleteOpen} title={copy.deleteTitle} message={copy.deleteMessage} confirmLabel={copy.remove} busy={submitter.busy} onconfirm={() => { void softDelete(); }} oncancel={() => { deleteTarget = null; }} />
<ConfirmDialog bind:open={conflict.open} title={copy.conflictTitle} message={copy.conflictMessage} confirmLabel={conflict.failed ? labels.retry : conflict.current?.period.status === 'cancelled' ? labels.back : copy.conflictContinue} busy={conflict.loading} onconfirm={acceptConflict} oncancel={() => conflict.dismiss()}>
  {#if conflict.loading}<p role="status">{labels.loading}</p>{/if}
  {#if conflict.failed}<p role="alert">{copy.conflictFailed}</p>{/if}
  {#if conflict.current?.rule}
    {@const current = conflict.current.rule}
    <div class="notice"><p>{copy.latest}：{copy.detail(current.days_before, current.reminder_time)} · {current.enabled ? copy.enabledState : copy.disabledState}</p><p>{copy.revision} {conflictTarget?.revision} → {current.revision}</p></div>
  {:else if duplicate && !conflict.loading && !conflict.failed}
    <div class="notice"><p>{copy.latest}：{copy.detail(duplicate.days_before, duplicate.reminder_time)} · {duplicate.enabled ? copy.enabledState : copy.disabledState}</p><p>{copy.revision} {duplicate.revision}</p></div>
  {/if}
  {#if conflict.current?.period.status === 'cancelled'}<p class="notice">{copy.cancelled}</p>{/if}
</ConfirmDialog>

<style>
  .rules-page h1,.rules-page h2,.rules-page p{margin:0}.rules-page__rule{padding:14px 0;border-bottom:1px solid var(--border);display:grid;gap:10px;min-width:0}.rules-page__summary{display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap}.rules-page__summary h2{overflow-wrap:anywhere}.rules-page__status{background:var(--badge-ok-bg);color:var(--badge-ok-fg);padding:4px 8px;border-radius:8px;font-size:12px}.rules-page__disabled{border-left:3px solid var(--border-strong);padding-left:10px}.rules-page__disabled .rules-page__status{background:var(--border);color:var(--text-secondary)}.rules-page__checkbox{display:flex;align-items:center;gap:10px;min-height:44px}
</style>
