<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../lib/api/resources';
  import type { User } from '../lib/api/types';
  import { acceptMe, canWrite, isTerminal, session } from '../lib/session.svelte';
  import { ensureSession } from '../lib/session-load';
  import ReadOnlyNotice from '../components/ReadOnlyNotice.svelte';
  import { Submitter } from '../lib/form.svelte';
  import { navigate } from '../lib/router';
  import { isValidTimeZone } from '../lib/domain/datetime';
  import { DEFAULT_SESSION_TTL, errorMessage, labels, sessionTTLOptions, settingsLabels } from '../lib/domain/labels';
  import AsyncState from '../components/AsyncState.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import TimeZoneSelect from '../components/TimeZoneSelect.svelte';
  import FormField from '../components/FormField.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let me = $state<User | null>(session.me);
  let timezone = $state(session.me?.timezone ?? '');
  let sessionTTL = $state(String(session.me?.session_ttl_seconds ?? DEFAULT_SESSION_TTL));
  let loading = $state(false);
  let loadError = $state('');
  let timezoneError = $state('');
  let confirmOpen = $state(false);
  const submitter = new Submitter();
  const sessionSubmitter = new Submitter();
  let controller: AbortController | undefined;
  let request = 0;
  let mounted = false;
  const timezoneDirty = $derived(me !== null && timezone !== me.timezone);
  const sessionDirty = $derived(me !== null && sessionTTL !== String(me.session_ttl_seconds ?? DEFAULT_SESSION_TTL));
  const dirty = $derived(timezoneDirty || sessionDirty);
  const readAllowed = $derived(session.auth !== 'missing' && !isTerminal());
  const disabled = $derived(!canWrite() || loading || submitter.busy || sessionSubmitter.busy);
  const sessionDisabled = $derived(!canWrite() || loading || sessionSubmitter.busy);

  $effect(() => { session.dirty = dirty; });

  async function loadMe() {
    if (!readAllowed || loading || submitter.busy || sessionSubmitter.busy) return;
    controller?.abort();
    controller = new AbortController();
    const signal = controller.signal;
    const current = ++request;
    const preserveTimezone = timezoneDirty;
    const preserveSession = sessionDirty;
    loading = true;
    loadError = '';
    try {
      const user = await api.me(signal);
      if (!mounted || signal.aborted || current !== request || !readAllowed) return;
      me = user;
      acceptMe(user);
      if (!preserveTimezone) timezone = user.timezone;
      if (!preserveSession) sessionTTL = String(user.session_ttl_seconds ?? DEFAULT_SESSION_TTL);
    } catch (error) {
      if (mounted && !signal.aborted && current === request) loadError = errorMessage(error);
    } finally {
      if (mounted && current === request) loading = false;
    }
  }

  function confirmChange(event: SubmitEvent) {
    event.preventDefault();
    if (disabled || !timezoneDirty) return;
    submitter.clear();
    timezoneError = isValidTimeZone(timezone) ? '' : settingsLabels.invalidTimezone;
    if (!timezoneError) confirmOpen = true;
  }

  async function saveTimezone() {
    if (disabled || !timezoneDirty || !isValidTimeZone(timezone)) {
      confirmOpen = false;
      return;
    }
    controller?.abort();
    controller = new AbortController();
    const signal = controller.signal;
    const value = timezone;
    const saved = await submitter.run(async () => {
      const user = await api.updateMe({ timezone: value }, signal);
      if (!mounted || signal.aborted) return;
      acceptMe(user);
      if (session.auth !== 'ready') return;
      me = user;
      timezone = user.timezone;
      session.dirty = sessionDirty;
    });
    if (!mounted) return;
    confirmOpen = false;
    if (saved && !signal.aborted && session.auth === 'ready' && !sessionDirty) navigate('/app/');
  }

  function saveSession(event: SubmitEvent) {
    event.preventDefault();
    if (sessionDisabled || !sessionDirty) return;
    sessionSubmitter.clear();
    const value = Number(sessionTTL);
    void sessionSubmitter.run(async () => {
      const user = await api.updateMe({ session_ttl_seconds: value });
      if (!mounted) return;
      acceptMe(user);
      if (session.auth !== 'ready') return;
      me = user;
      sessionTTL = String(user.session_ttl_seconds ?? DEFAULT_SESSION_TTL);
      session.dirty = timezoneDirty;
    });
  }

  onMount(() => {
    mounted = true;
    void loadMe();
    return () => {
      mounted = false;
      request += 1;
      controller?.abort();
      session.dirty = false;
    };
  });
</script>

<div class="content stack settings">
  <h1>{labels.settings}</h1>
  {#if !canWrite()}<ReadOnlyNotice />{/if}
  {#if loadError}<ErrorBanner message={loadError} onretry={readAllowed ? () => { void loadMe(); } : undefined} />{/if}
  <AsyncState loading={loading && !me}>
    {#if me}
      <section class="section stack" aria-label={settingsLabels.currentTimezone}>
        <form class="stack" onsubmit={confirmChange} novalidate>
          <TimeZoneSelect id="settings-timezone" bind:value={timezone} {disabled} error={timezoneError} />
          {#if submitter.error}<ErrorBanner message={errorMessage(submitter.error)} />{/if}
          <div class="actions"><button class="primary" type="submit" disabled={disabled || !timezoneDirty}>{settingsLabels.saveTimezone}</button></div>
        </form>
      </section>
      <section class="section stack" aria-label={settingsLabels.sessionValidity}>
        <form class="stack" onsubmit={saveSession}>
          <FormField id="settings-session-ttl" label={settingsLabels.sessionValidity}>
            {#snippet children(field)}
              <select id={field.id} bind:value={sessionTTL} disabled={sessionDisabled}>
                {#each sessionTTLOptions as option (option.value)}
                  <option value={option.value}>{option.label}</option>
                {/each}
              </select>
            {/snippet}
          </FormField>
          {#if sessionSubmitter.error}<ErrorBanner message={errorMessage(sessionSubmitter.error)} />{/if}
          <div class="actions"><button class="primary" type="submit" disabled={sessionDisabled || !sessionDirty}>{settingsLabels.saveSession}</button></div>
        </form>
      </section>
      <section class="section stack" aria-label={settingsLabels.privateChat}>
        <p role="status">{me.has_private_chat ? settingsLabels.bound : settingsLabels.unbound}</p>
        {#if !me.has_private_chat}<p>{settingsLabels.bindHint}</p>{/if}
        <div class="actions"><button type="button" disabled={!readAllowed || loading || submitter.busy || sessionSubmitter.busy} onclick={() => { void loadMe(); }}>{loading ? labels.loading : settingsLabels.refreshBinding}</button></div>
      </section>
    {/if}
  </AsyncState>
  <section class="section stack">
    <div class="actions"><button type="button" onclick={() => navigate('/app/reminders')}>{settingsLabels.reminders}</button></div>
  </section>
</div>

<ConfirmDialog bind:open={confirmOpen} title={settingsLabels.confirmTimezone} message={settingsLabels.timezoneImpact} confirmLabel={settingsLabels.confirmSave} busy={submitter.busy} onconfirm={() => { void saveTimezone(); }} oncancel={() => { confirmOpen = false; }}>
  <p>{me?.timezone} → {timezone}</p>
</ConfirmDialog>

<style>
  p{margin:0;overflow-wrap:anywhere}.settings{min-width:0}
</style>
