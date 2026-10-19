<script lang="ts">
  import { onMount } from 'svelte';
  import { beginSession, session } from './lib/session.svelte';
  import { back, matchRoute, navigate, replace, setLeaveGuard, subscribe } from './lib/router';
  import { getInitData, getWebApp, setClosingConfirmation, startTelegram, supportsVersion } from './lib/telegram/sdk';
  import { startTheme } from './lib/telegram/theme';
  import { labels } from './lib/domain/labels';
  import AppShell from './components/AppShell.svelte';
  import AppHeader from './components/AppHeader.svelte';
  import ErrorBanner from './components/ErrorBanner.svelte';
  import Home from './routes/Home.svelte';
  import ServiceDetail from './routes/ServiceDetail.svelte';
  import ServiceForm from './routes/ServiceForm.svelte';
  import PeriodForm from './routes/PeriodForm.svelte';
  import Rules from './routes/Rules.svelte';
  import Renew from './routes/Renew.svelte';
  import Reminders from './routes/Reminders.svelte';
  import Settings from './routes/Settings.svelte';
  import { testMode } from './lib/test-mode';
  import { api } from './lib/api/resources';
  let path = $state(location.pathname + location.search);
  const route = $derived(matchRoute(path));
  let legacyFailure = $state(false);
  let legacyController: AbortController | null = null;
  let legacyGeneration = 0;
  $effect(() => {
    const current = route;
    const sourcePath = path;
    if (!current.name.startsWith('legacy-sim') && current.name !== 'legacy-period-new') {
      legacyController?.abort(); legacyController = null; legacyGeneration++; legacyFailure = false;
      return;
    }
    if (current.name === 'legacy-sim-new') { replace('/app/services/new?category=sim'); return; }
    const id = current.params.id;
    if (!id) return;
    if (!getInitData()) return;
    const generation = ++legacyGeneration;
    const controller = new AbortController();
    legacyController?.abort(); legacyController = controller;
    legacyFailure = false;
    void api.sim(id, controller.signal).then(() => {
      if (controller.signal.aborted || generation !== legacyGeneration || path !== sourcePath) return;
      const target = current.name === 'legacy-period-new' ? `/app/services/${id}/periods/new` : current.name === 'legacy-sim-edit' ? `/app/services/${id}/edit` : `/app/services/${id}`;
      replace(target);
    }).catch(() => { if (!controller.signal.aborted && generation === legacyGeneration && path === sourcePath) legacyFailure = true; });
    return () => controller.abort();
  });

  $effect(() => { setClosingConfirmation(session.dirty); });

  onMount(() => {
    const stopTelegram = startTelegram();
    const stopTheme = startTheme();
    const stopGuard = setLeaveGuard(() => !session.dirty || window.confirm(labels.leaveConfirm));
    const returnHome = () => back('/app/');
    const sdk = getWebApp();
    const button = sdk && supportsVersion(sdk, '6.1') ? sdk.BackButton : undefined;
    button?.onClick(returnHome);
    const unsubscribe = subscribe((next) => {
      path = next;
      if (matchRoute(next).name === 'home') button?.hide();
      else button?.show();
    });
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!session.dirty) return;
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', beforeUnload);
    beginSession(getInitData().length > 0);
    return () => {
      window.removeEventListener('beforeunload', beforeUnload);
      unsubscribe();
      stopGuard();
      button?.offClick(returnHome);
      button?.hide();
      setClosingConfirmation(false);
      stopTheme();
      stopTelegram();
    };
  });
</script>

<AppShell>
  {#if testMode}
    <aside class="test-mode" role="status">本地测试模式 · 仅测试数据 · 重启后重置 · 不发送提醒</aside>
  {/if}
  {#if session.auth === 'missing'}
    <AppHeader firstName="" photoUrl={null} onsettings={() => navigate('/app/settings')}>{#snippet children()}{/snippet}</AppHeader>
    <div class="content stack">
      <section class="section"><h2>{labels.openTelegram}</h2><p>{labels.openTelegramHint}</p></section>
    </div>
  {:else if route.name === 'home'}
    {#key route.name}<Home />{/key}
  {:else if route.name === 'legacy-sim-new' || route.name === 'legacy-sim-detail' || route.name === 'legacy-sim-edit' || route.name === 'legacy-period-new'}
    <div class="content stack">{#if legacyFailure}<section class="section"><h2>{labels.notFound}</h2></section>{:else}<p role="status">{labels.loading}</p>{/if}</div>
  {:else if route.name === 'service-detail' && route.params.id}
    {#key `${route.name}:${route.params.id}`}<ServiceDetail id={route.params.id} />{/key}
  {:else if route.name === 'service-new'}
    {#key path}<ServiceForm initialKind={new URL(path, location.origin).searchParams.get('category') === 'sim' ? 'sim' : undefined} />{/key}
  {:else if route.name === 'service-edit' && route.params.id}
    {#key `${route.name}:${route.params.id}`}<ServiceForm id={route.params.id} />{/key}
  {:else if route.name === 'period-new' && route.params.id}
    {#key `${route.name}:${route.params.id}`}<PeriodForm id={route.params.id} mode="new" />{/key}
  {:else if route.name === 'period-edit' && route.params.id}
    {#key `${route.name}:${route.params.id}`}<PeriodForm id={route.params.id} mode="edit" />{/key}
  {:else if route.name === 'rules' && route.params.id}
    {#key `${route.name}:${route.params.id}`}<Rules id={route.params.id} />{/key}
  {:else if route.name === 'renew' && route.params.id}
    {#key `${route.name}:${route.params.id}`}<Renew id={route.params.id} />{/key}
  {:else if route.name === 'reminders'}
    {#key route.name}<Reminders />{/key}
  {:else if route.name === 'settings'}
    {#key route.name}<Settings />{/key}
  {:else}
    <AppHeader firstName="" photoUrl={null} onsettings={() => navigate('/app/settings')}>{#snippet children()}{/snippet}</AppHeader>
    <div class="content stack">
      {#if session.auth === 'expired' || session.auth === 'forbidden'}
        <ErrorBanner message={session.auth === 'expired' ? labels.expired : labels.forbidden} />
      {/if}
      <section class="section"><h2>{labels.notFound}</h2><p>{labels.notFoundHint}</p></section>
      <div class="actions"><button onclick={() => navigate('/app/')}>{labels.home}</button></div>
    </div>
  {/if}
</AppShell>
