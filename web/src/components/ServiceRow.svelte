<script lang="ts">
  import type { Category, Period, ServiceItem } from '../lib/api/types';
  import { formatExpiry } from '../lib/domain/expiry';
  import { cardTypeLabels, labels, periodLabel, serviceStatusLabels } from '../lib/domain/labels';
  import { maskPhone } from '../lib/domain/mask';
  import ExpiryBadge from './ExpiryBadge.svelte';
  import IconTile from './IconTile.svelte';

  let { service, period, category, onopen }: {
    service: ServiceItem;
    period: Period | null;
    category: Category | undefined;
    onopen: (id: string) => void;
  } = $props();
  const isSim = $derived(service.sim_details !== null);
  const secondary = $derived([category?.name, service.provider?.trim(), isSim ? maskPhone(service.sim_details?.phone_number) : service.identifier?.trim()].filter(Boolean).join(' · '));
</script>

<button type="button" class="service-row" class:archived={service.status === 'archived'} onclick={() => onopen(service.id)}>
  <IconTile {category} cardType={service.sim_details?.card_type ?? null} />
  <span class="details">
    <span class="name-line">
      <span class="name">{service.name}</span>
      {#if service.sim_details}<span class="kind">{cardTypeLabels[service.sim_details.card_type]}</span>{/if}
      {#if service.status === 'archived'}<span class="archive-badge">{serviceStatusLabels.archived}</span>{/if}
    </span>
    {#if secondary}<span class="secondary">{secondary}</span>{/if}
    {#if period}
      <span class="expiry">{periodLabel(period)}：{formatExpiry(period)}</span>
      <span class="status-line"><ExpiryBadge {period} /></span>
    {:else}
      <span class="expiry">{labels.noPeriod}</span>
    {/if}
  </span>
  <svg class="chevron" width="14" height="18" viewBox="0 0 16 20" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 5 5 5-5 5" /></svg>
</button>

<style>
  .service-row{position:relative;display:flex;align-items:center;gap:10px;width:100%;min-height:88px;margin:0;padding:12px 0;border:0;border-radius:6px;background:transparent;text-align:left;color:var(--text)}
  .service-row::after{content:'';position:absolute;inset:auto 0 0;height:1px;background:var(--border)}
  .archived{opacity:.6}
  .details{display:grid;gap:4px;flex:1;min-width:0;overflow-wrap:anywhere}
  .name-line{display:flex;align-items:baseline;flex-wrap:wrap;gap:5px 7px;min-width:0}
  .name{font-size:13px;font-weight:600;line-height:1.4;overflow-wrap:anywhere}
  .kind{font-size:10px;line-height:1.4;color:var(--text-secondary)}
  .archive-badge{font-size:10px;font-weight:600;padding:2px 6px;border-radius:999px;color:var(--text-secondary);background:var(--border)}
  .secondary{font-size:12px;line-height:1.4;color:var(--text-secondary);white-space:pre-wrap}
  .expiry{font-size:11px;line-height:1.4;color:var(--text-secondary)}
  .status-line{display:flex;align-items:center;gap:6px;flex-wrap:wrap}
  .chevron{flex:0 0 14px;color:var(--text-secondary)}
</style>
