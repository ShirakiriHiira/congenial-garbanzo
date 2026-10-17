<script lang="ts">
  import type { OverviewCounts } from '../lib/api/types';
  import { labels } from '../lib/domain/labels';

  let { counts, unavailable }: { counts: OverviewCounts | null; unavailable: boolean } = $props();
</script>
<div class="stat-group" role="group" aria-label={labels.statistics} aria-busy={!unavailable && counts === null}>
  <dl class="stat-grid">
    <div>
      <dt>{labels.total}</dt>
      <dd>{unavailable || counts === null ? '—' : counts.total}</dd>
    </div>
    <div>
      <dt>{labels.dueSoon}</dt>
      <dd class="warn">{unavailable || counts === null ? '—' : counts.due_soon}</dd>
    </div>
    <div>
      <dt>{labels.expiredCount}</dt>
      <dd class="danger">{unavailable || counts === null ? '—' : counts.expired}</dd>
    </div>
  </dl>
</div>

<style>
  .stat-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:6px;margin:0}
  .stat-grid>div{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:4px;min-width:0;min-height:50px;padding:7px 4px;border-radius:10px;background:var(--stat-bg);text-align:center}
  dt{font-size:10px;line-height:1.4;color:var(--text-muted);overflow-wrap:anywhere}
  dd{margin:0;font-size:16px;font-weight:700;line-height:1.2;color:var(--text-on-dark);overflow-wrap:anywhere}
  dd.warn{color:var(--accent-warn)}
  dd.danger{color:var(--accent-danger)}
</style>
