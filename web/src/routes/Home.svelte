<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../lib/api/resources';
  import type { OverviewItem, OverviewResponse, Page, Sim } from '../lib/api/types';
  import { acceptMe, canWrite, onSessionFailure, session } from '../lib/session.svelte';
  import { navigate } from '../lib/router';
  import { getInitData } from '../lib/telegram/sdk';
  import { parseDisplayUser } from '../lib/telegram/initdata';
  import { expiryDisplay } from '../lib/domain/expiry';
  import { errorMessage, labels } from '../lib/domain/labels';
  import AppHeader from '../components/AppHeader.svelte';
  import StatGrid from '../components/StatGrid.svelte';
  import SectionHeader from '../components/SectionHeader.svelte';
  import SimList from '../components/SimList.svelte';
  import FilterSheet, { type FilterValue } from '../components/FilterSheet.svelte';
  import StickyFooter from '../components/StickyFooter.svelte';
  import AsyncState from '../components/AsyncState.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';

  const displayUser = parseDisplayUser(getInitData());
  let overview = $state<OverviewResponse | null>(null);
  let archives = $state<Page<Sim> | null>(null);
  let filter = $state<FilterValue>('all');
  let sheetOpen = $state(false);
  let loading = $state(true);
  let loadingMore = $state(false);
  let archiveLoading = $state(false);
  let archiveMore = $state(false);
  let accountLoading = $state(true);
  let accountError = $state(false);
  let overviewError = $state('');
  let overviewMoreFailed = $state(false);
  let archiveError = $state('');
  let sequence = 0;
  let archiveSequence = 0;
  let controller: AbortController | undefined;
  let archiveController: AbortController | undefined;
  const terminal = $derived(session.auth === 'expired' || session.auth === 'forbidden');
  const archived = $derived(filter === 'archived');
  const items = $derived<OverviewItem[]>(archived
    ? (archives?.data ?? []).filter((sim) => sim.status === 'archived').map((sim) => ({ sim, primary_period: null }))
    : (overview?.data.items ?? []).filter((item) => filter === 'all'
      || (filter === 'expired'
        ? item.primary_period?.status === 'expired'
        : item.primary_period?.status === 'active' && expiryDisplay(item.primary_period).tone === 'warn')));
  const hasMore = $derived(Boolean(archived ? archives?.next_cursor : overview?.next_cursor));
  const busy = $derived(archived ? archiveLoading : loading);
  const moreBusy = $derived(archived ? archiveMore : loadingMore);
  const loaded = $derived(archived ? archives !== null : overview !== null);
  const visibleError = $derived(archived ? archiveError : overviewError);
  const subset = $derived(!archived && filter !== 'all');
  const total = $derived(overview?.data.counts.total ?? 0);
  const footerLabel = $derived(archived ? labels.archivedLoaded(items.length)
    : subset ? labels.loadedOfTotal(items.length, total)
    : labels.cardsTotal(overview?.data.counts.total ?? '—'));

  function stopRequests(): void {
    ++sequence;
    ++archiveSequence;
    controller?.abort();
    archiveController?.abort();
    loading = loadingMore = archiveLoading = archiveMore = accountLoading = false;
  }

  async function refresh(): Promise<void> {
    if (terminal || getInitData().length === 0) {
      loading = accountLoading = false;
      return;
    }
    controller?.abort();
    controller = new AbortController();
    const signal = controller.signal;
    const request = ++sequence;
    const current = () => request === sequence && !signal.aborted && !terminal;
    loading = accountLoading = true;
    loadingMore = accountError = false;
    overviewError = '';
    overviewMoreFailed = false;
    await Promise.all([
      api.me(signal).then((user) => { if (current()) acceptMe(user); })
        .catch(() => { if (current()) accountError = true; })
        .finally(() => { if (current()) accountLoading = false; }),
      api.overview(null, signal).then((result) => { if (current()) overview = result; })
        .catch((error: unknown) => { if (current()) overviewError = errorMessage(error); })
        .finally(() => { if (current()) loading = false; }),
      archived ? loadArchives(false) : Promise.resolve(),
    ]);
  }

  async function loadMore(): Promise<void> {
    if (archived) {
      await loadArchives(true);
      return;
    }
    if (terminal || loading || loadingMore || !overview?.next_cursor) return;
    const request = sequence;
    const signal = controller?.signal;
    const current = () => request === sequence && !signal?.aborted && !terminal;
    loadingMore = true;
    overviewError = '';
    try {
      const result = await api.overview(overview.next_cursor, signal);
      if (!current() || !overview) return;
      const known = new Set(overview.data.items.map((item) => item.sim.id));
      overview = {
        ...result,
        data: {
          ...result.data,
          items: [...overview.data.items, ...result.data.items.filter((item) => !known.has(item.sim.id))],
        },
      };
    } catch (error) {
      if (current()) {
        overviewError = errorMessage(error);
        overviewMoreFailed = true;
      }
    } finally {
      if (current()) loadingMore = false;
    }
  }

  async function loadArchives(append: boolean): Promise<void> {
    if (terminal || (append && (archiveLoading || archiveMore || !archives?.next_cursor))) return;
    archiveController?.abort();
    archiveController = new AbortController();
    const signal = archiveController.signal;
    const request = ++archiveSequence;
    const current = () => request === archiveSequence && !signal.aborted && !terminal;
    const cursor = append ? archives?.next_cursor ?? null : null;
    archiveError = '';
    archiveLoading = !append;
    archiveMore = append;
    if (!append) archives = null;
    try {
      const result = await api.sims(cursor, signal);
      if (!current()) return;
      const known = new Set(archives?.data.map((sim) => sim.id) ?? []);
      archives = {
        ...result,
        data: append ? [...(archives?.data ?? []), ...result.data.filter((sim) => !known.has(sim.id))] : result.data,
      };
    } catch (error) {
      if (current()) archiveError = errorMessage(error);
    } finally {
      if (current()) archiveLoading = archiveMore = false;
    }
  }

  function selectFilter(value: FilterValue): void {
    if (value === filter) return;
    if (archived) {
      ++archiveSequence;
      archiveController?.abort();
      archiveLoading = archiveMore = false;
    }
    filter = value;
    if (value === 'archived' && archives === null) void loadArchives(false);
  }

  onMount(() => {
    const unsubscribe = onSessionFailure(stopRequests);
    void refresh();
    return () => {
      unsubscribe();
      stopRequests();
    };
  });
</script>

<AppHeader firstName={displayUser.firstName} photoUrl={displayUser.photoUrl} onsettings={() => navigate('/app/settings')}>
  <StatGrid counts={overview?.data.counts ?? null} unavailable={!overview && !loading} />
</AppHeader>
<div class="content stack">
  {#if terminal}
    <ErrorBanner message={session.auth === 'expired' ? labels.expired : labels.forbidden} />
  {/if}
  {#if accountLoading && !terminal}<p role="status">{labels.loadingAccount}</p>{/if}
  {#if accountError && !terminal}<p class="notice" role="alert">{labels.accountFailed}</p>{/if}
  {#if overview && (!canWrite() || accountError)}<p class="notice">{labels.readOnly}</p>{/if}
  <SectionHeader title={labels.cardList}>
    <button type="button" onclick={() => { sheetOpen = true; }} disabled={terminal}>{labels.filter}</button>
    {#if session.auth !== 'forbidden'}
      <button type="button" onclick={refresh} disabled={loading || loadingMore || archiveLoading || archiveMore || terminal}>{labels.refresh}</button>
    {/if}
  </SectionHeader>
  {#if subset && overview?.next_cursor}<p class="notice">{labels.filterLoadedOnly}</p>{/if}
  {#if visibleError && loaded && !terminal}
    <ErrorBanner message={`${labels.incomplete} ${visibleError}`}
      onretry={archived ? () => loadArchives(true) : overviewMoreFailed ? loadMore : refresh} />
  {/if}
  <AsyncState loading={busy && !loaded} error={loaded || terminal ? '' : visibleError}
    empty={loaded && !busy && items.length === 0}
    emptyMessage={archived ? labels.archivedEmpty
      : subset ? labels.filterEmpty
      : overview?.data.counts.total === 0 ? labels.emptyCards : labels.filterEmpty}
    onretry={archived ? () => loadArchives(false) : refresh}>
    <SimList {items} onopen={(id) => navigate(`/app/sims/${id}`)} />
  </AsyncState>
  {#if hasMore && !terminal}
    <button type="button" class="load-more" onclick={loadMore} disabled={busy || moreBusy}>
      {moreBusy ? labels.loading : labels.loadMore}
    </button>
  {/if}
</div>
<StickyFooter label={footerLabel} onadd={() => navigate('/app/sims/new')}
  disabled={!canWrite() || accountError || terminal} />
<FilterSheet bind:open={sheetOpen} value={filter} loadedCount={items.length}
  total={archived ? items.length : total} {hasMore} onselect={selectFilter} />

<style>
  .load-more { width: 100%; }
</style>
