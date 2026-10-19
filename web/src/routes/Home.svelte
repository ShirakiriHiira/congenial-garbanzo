<script lang="ts">
  import { onMount } from 'svelte';
  import Filter from 'lucide-svelte/icons/filter';
  import RefreshCw from 'lucide-svelte/icons/refresh-cw';
  import Search from 'lucide-svelte/icons/search';
  import { api, fetchAllPages } from '../lib/api/resources';
  import type { Category, ServiceFilter, ServiceOverviewResponse } from '../lib/api/types';
  import { acceptMe, canWrite, onSessionFailure, session } from '../lib/session.svelte';
  import { navigate } from '../lib/router';
  import { getInitData } from '../lib/telegram/sdk';
  import { parseDisplayUser } from '../lib/telegram/initdata';
  import { errorMessage, labels } from '../lib/domain/labels';
  import AppHeader from '../components/AppHeader.svelte';
  import StatGrid from '../components/StatGrid.svelte';
  import SectionHeader from '../components/SectionHeader.svelte';
  import ServiceList from '../components/ServiceList.svelte';
  import FilterSheet, { type HomeStatus } from '../components/FilterSheet.svelte';
  import StickyFooter from '../components/StickyFooter.svelte';
  import AsyncState from '../components/AsyncState.svelte';
  import ErrorBanner from '../components/ErrorBanner.svelte';

  const copy = {
    clear: '清除筛选',
    incomplete: '列表加载不完整，已保留已加载的服务。',
  } as const;

  const displayUser = parseDisplayUser(getInitData());
  let collection = $state<ServiceOverviewResponse | null>(null);
  let categories = $state<Category[]>([]);
  let categoriesError = $state('');
  let status = $state<HomeStatus>('active');
  let categoryID = $state<string | null>(null);
  let draftQuery = $state('');
  let query = $state('');
  let appliedFilter = $state<ServiceFilter>({ status: 'active' });
  let sheetOpen = $state(false);
  let loading = $state(true);
  let loadingMore = $state(false);
  let categoryLoading = $state(true);
  let accountLoading = $state(true);
  let accountError = $state(false);
  let error = $state('');
  let moreFailed = $state(false);
  let pageSequence = 0;
  let categorySequence = 0;
  let accountSequence = 0;
  let pageController: AbortController | undefined;
  let categoryController: AbortController | undefined;
  let accountController: AbortController | undefined;
  const terminal = $derived(session.auth === 'expired' || session.auth === 'forbidden');
  const items = $derived(collection?.data.items ?? []);
  const hasMore = $derived(Boolean(collection?.next_cursor));
  const loaded = $derived(collection !== null);
  const total = $derived(collection?.data.counts.total ?? 0);
  const filtered = $derived(query !== '' || categoryID !== null || status !== 'active');

  function currentFilter(): ServiceFilter {
    const value: ServiceFilter = { status };
    const text = query.trim();
    if (text !== '') value.q = text;
    if (categoryID !== null) value.category_id = categoryID;
    return value;
  }

  function stopRequests(): void {
    ++pageSequence;
    ++categorySequence;
    ++accountSequence;
    pageController?.abort();
    categoryController?.abort();
    accountController?.abort();
    loading = loadingMore = categoryLoading = accountLoading = false;
  }

  async function loadAccount(): Promise<void> {
    accountController?.abort();
    accountController = new AbortController();
    const signal = accountController.signal;
    const request = ++accountSequence;
    const current = () => request === accountSequence && !signal.aborted && !terminal;
    accountLoading = true;
    accountError = false;
    try {
      const user = await api.me(signal);
      if (current()) acceptMe(user);
    } catch {
      if (current()) accountError = true;
    } finally {
      if (current()) accountLoading = false;
    }
  }

  async function loadCategories(): Promise<void> {
    categoryController?.abort();
    categoryController = new AbortController();
    const signal = categoryController.signal;
    const request = ++categorySequence;
    const current = () => request === categorySequence && !signal.aborted && !terminal;
    categoryLoading = true;
    categoriesError = '';
    try {
      const result = await fetchAllPages((after) => api.categories(after, signal));
      if (current()) categories = result;
    } catch (cause) {
      if (current()) categoriesError = errorMessage(cause);
    } finally {
      if (current()) categoryLoading = false;
    }
  }

  async function loadFirst(): Promise<void> {
    if (terminal || getInitData().length === 0) {
      loading = false;
      return;
    }
    pageController?.abort();
    pageController = new AbortController();
    const signal = pageController.signal;
    const request = ++pageSequence;
    const current = () => request === pageSequence && !signal.aborted && !terminal;
    const filter = currentFilter();
    appliedFilter = filter;
    loading = true;
    loadingMore = false;
    error = '';
    moreFailed = false;
    collection = null;
    try {
      const result = await api.serviceOverview(null, filter, signal);
      if (current()) collection = result;
    } catch (cause) {
      if (current()) error = errorMessage(cause);
    } finally {
      if (current()) loading = false;
    }
  }

  async function refresh(): Promise<void> {
    if (terminal || getInitData().length === 0) {
      loading = accountLoading = categoryLoading = false;
      return;
    }
    await Promise.all([loadAccount(), loadCategories(), loadFirst()]);
  }

  async function loadMore(): Promise<void> {
    if (terminal || loading || loadingMore || !collection?.next_cursor) return;
    const signal = pageController?.signal;
    const request = pageSequence;
    const current = () => request === pageSequence && !signal?.aborted && !terminal;
    const cursor = collection.next_cursor;
    const filter = appliedFilter;
    loadingMore = true;
    error = '';
    moreFailed = false;
    try {
      const result = await api.serviceOverview(cursor, filter, signal);
      if (!current() || !collection) return;
      const known = new Set(collection.data.items.map((item) => item.service.id));
      collection = {
        ...result,
        data: {
          ...result.data,
          items: [...collection.data.items, ...result.data.items.filter((item) => !known.has(item.service.id))],
        },
      };
    } catch (cause) {
      if (current()) {
        error = errorMessage(cause);
        moreFailed = true;
      }
    } finally {
      if (current()) loadingMore = false;
    }
  }

  function applySearch(): void {
    const next = draftQuery.trim();
    if (next === query) {
      draftQuery = next;
      return;
    }
    query = next;
    draftQuery = next;
    void loadFirst();
  }

  function clearFilters(): void {
    draftQuery = '';
    if (draftQuery === '' && query === '' && categoryID === null && status === 'active') return;
    query = '';
    categoryID = null;
    status = 'active';
    void loadFirst();
  }

  function selectStatus(value: HomeStatus): void {
    if (value === status) return;
    status = value;
    void loadFirst();
  }

  function selectCategory(value: string): void {
    const next = value === '' ? null : value;
    if (next === categoryID) return;
    categoryID = next;
    void loadFirst();
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
  <StatGrid counts={collection?.data.counts ?? null} unavailable={!collection && !loading} />
</AppHeader>
<div class="content stack">
  {#if terminal}
    <ErrorBanner message={session.auth === 'expired' ? labels.expired : labels.forbidden} />
  {/if}
  {#if accountLoading && !terminal}<p role="status">{labels.loadingAccount}</p>{/if}
  {#if accountError && !terminal}<p class="notice" role="alert">{labels.accountFailed}</p>{/if}
  {#if collection && (!canWrite() || accountError)}<p class="notice">{labels.readOnly}</p>{/if}
  <form class="search" role="search" onsubmit={(event) => { event.preventDefault(); applySearch(); }}>
    <label class="search-field">
      <Search size={17} strokeWidth={1.8} aria-hidden="true" />
      <input type="search" maxlength="200" bind:value={draftQuery} placeholder={labels.searchPlaceholder} aria-label={labels.search} />
    </label>
    <button type="submit" class="search-submit" disabled={terminal}>{labels.search}</button>
  </form>
  <SectionHeader title={labels.cardList}>
    <button type="button" class="icon-button" aria-label={labels.filter} title={labels.filter} onclick={() => { sheetOpen = true; }} disabled={terminal}><Filter size={19} strokeWidth={1.8} aria-hidden="true" /></button>
    {#if session.auth !== 'forbidden'}
      <button type="button" class="icon-button" aria-label={labels.refresh} title={labels.refresh} onclick={refresh} disabled={loading || loadingMore || terminal}><RefreshCw size={19} strokeWidth={1.8} aria-hidden="true" /></button>
    {/if}
  </SectionHeader>
  {#if filtered || draftQuery !== ''}
    <button type="button" class="filter-actions" onclick={clearFilters} disabled={loading || terminal}>{copy.clear}</button>
  {/if}
  {#if error && loaded && !terminal}
    <ErrorBanner message={`${copy.incomplete} ${error}`}
      onretry={moreFailed ? loadMore : refresh} />
  {/if}
  <AsyncState loading={loading && !loaded} error={loaded || terminal ? '' : error}
    empty={loaded && !loading && items.length === 0}
    emptyMessage={status === 'archived' || filtered ? labels.filterEmpty : total === 0 ? labels.emptyCards : labels.filterEmpty}
    onretry={refresh}>
    <ServiceList {items} {categories} onopen={(id) => navigate(`/app/services/${id}`)} />
  </AsyncState>
  {#if hasMore && !terminal}
    <button type="button" class="load-more" onclick={loadMore} disabled={loading || loadingMore}>
      {loadingMore ? labels.loading : labels.loadMore}
    </button>
  {/if}
</div>
<StickyFooter label={labels.cardsTotal(loaded ? total : '—')} onadd={() => navigate('/app/services/new')}
  disabled={!canWrite() || accountError || terminal} />
<FilterSheet bind:open={sheetOpen} {status} {categoryID} {categories} {categoriesError}
  counts={collection?.data.counts ?? null} {categoryLoading}
  onstatus={selectStatus} oncategory={selectCategory} onreloadcategories={loadCategories} />

<style>
  .search{display:flex;align-items:center;gap:8px;min-width:0;margin:0}
  .search-field{display:flex;align-items:center;gap:7px;flex:1;min-width:0;min-height:44px;padding:0 12px;border:1px solid var(--border-strong);border-radius:999px;background:var(--page-bg);color:var(--text-secondary)}
  .search-field input{flex:1;min-width:0;height:42px;border:0;padding:0;background:transparent;color:var(--text);outline:none}
  .search-field input::placeholder{color:var(--text-secondary);opacity:.8}
  .search-submit{flex:0 0 auto}
  .filter-actions{align-self:flex-start;font-size:12px}
  .load-more{width:100%}
  .icon-button{display:inline-flex;align-items:center;justify-content:center;width:44px;min-width:44px;height:44px;min-height:44px;padding:0}
</style>
