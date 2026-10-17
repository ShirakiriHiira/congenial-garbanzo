<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../lib/api/resources';
  import type { User } from '../lib/api/types';
  import { acceptMe, canWrite, session } from '../lib/session.svelte';
  import { Submitter } from '../lib/form.svelte';
  import { navigate } from '../lib/router';
  import { isValidTimeZone } from '../lib/domain/datetime';
  import { errorMessage, labels, settingsLabels } from '../lib/domain/labels';
  import AsyncState from '../components/AsyncState.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';
  import TimeZoneSelect from '../components/TimeZoneSelect.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let me = $state<User | null>(session.me);
  let timezone = $state(session.me?.timezone ?? '');
  let loading = $state(false);
  let loadError = $state('');
  let timezoneError = $state('');
  let confirmOpen = $state(false);
  const submitter = new Submitter();
  let controller: AbortController | undefined;
  let request = 0;
  let mounted = false;
  const dirty = $derived(me !== null && timezone !== me.timezone);
  const readAllowed = $derived(session.auth !== 'missing' && session.auth !== 'expired' && session.auth !== 'forbidden');
  const disabled = $derived(!canWrite() || loading || submitter.busy);

  $effect(() => { session.dirty = dirty; });

  async function loadMe() {
    if (!readAllowed || loading || submitter.busy) return;
    controller?.abort();
    controller = new AbortController();
    const signal = controller.signal;
    const current = ++request;
    const preserveDraft = dirty;
    loading = true;
    loadError = '';
    try {
      const user = await api.me(signal);
      if (!mounted || signal.aborted || current !== request || !readAllowed) return;
      me = user;
      acceptMe(user);
      if (!preserveDraft) timezone = user.timezone;
    } catch (error) {
      if (mounted && !signal.aborted && current === request) loadError = errorMessage(error);
    } finally {
      if (mounted && current === request) loading = false;
    }
  }

  function confirmChange(event: SubmitEvent) {
    event.preventDefault();
    if (disabled || !dirty) return;
    submitter.clear();
    timezoneError = isValidTimeZone(timezone) ? '' : settingsLabels.invalidTimezone;
    if (!timezoneError) confirmOpen = true;
  }

  async function saveTimezone() {
    if (disabled || !dirty || !isValidTimeZone(timezone)) {
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
      session.dirty = false;
    });
    if (!mounted) return;
    confirmOpen = false;
    if (saved && !signal.aborted && session.auth === 'ready') navigate('/app/');
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

<div class="stack settings">
  <h1>{labels.settings}</h1>
  {#if !canWrite()}<p class="notice">{labels.readOnly}</p>{/if}
  {#if loadError}<ErrorBanner message={loadError} onretry={readAllowed ? () => { void loadMe(); } : undefined} />{/if}
  <AsyncState loading={loading && !me}>
    {#if me}
      <section class="section stack" aria-label={settingsLabels.currentTimezone}>
        <h2>{settingsLabels.currentTimezone}</h2>
        <p class="settings__zone">{me.timezone}</p>
        <form class="stack" onsubmit={confirmChange} novalidate>
          <TimeZoneSelect id="settings-timezone" bind:value={timezone} {disabled} error={timezoneError} />
          {#if submitter.error}<ErrorBanner message={errorMessage(submitter.error)} />{/if}
          <div class="actions"><button class="primary" type="submit" disabled={disabled || !dirty}>{settingsLabels.saveTimezone}</button></div>
        </form>
      </section>
      <section class="section stack" aria-labelledby="private-chat-heading">
        <h2 id="private-chat-heading">{settingsLabels.privateChat}</h2>
        <p role="status">{me.has_private_chat ? settingsLabels.bound : settingsLabels.unbound}</p>
        {#if !me.has_private_chat}<p>{settingsLabels.bindHint}</p>{/if}
        <div class="actions"><button type="button" disabled={!readAllowed || loading || submitter.busy} onclick={() => { void loadMe(); }}>{loading ? labels.loading : settingsLabels.refreshBinding}</button></div>
      </section>
    {/if}
  </AsyncState>
  <section class="section stack">
    <h2>{settingsLabels.reminders}</h2>
    <p>{settingsLabels.remindersHint}</p>
    <div class="actions"><button type="button" onclick={() => navigate('/app/reminders')}>{settingsLabels.reminders}</button></div>
  </section>
  <section class="section stack" aria-labelledby="privacy-heading">
    <h2 id="privacy-heading">{settingsLabels.privacy}</h2>
    <p>{settingsLabels.privacyHint}</p>
  </section>
</div>

<ConfirmDialog bind:open={confirmOpen} title={settingsLabels.confirmTimezone} message={settingsLabels.timezoneImpact} confirmLabel={settingsLabels.confirmSave} busy={submitter.busy} onconfirm={() => { void saveTimezone(); }} oncancel={() => { confirmOpen = false; }}>
  <p>{me?.timezone} → {timezone}</p>
</ConfirmDialog>

<style>
  h1{font-size:20px;margin:0}h2{font-size:16px;margin:0}p{margin:0;overflow-wrap:anywhere}.settings__zone{font-weight:600}.settings{min-width:0}
</style>
