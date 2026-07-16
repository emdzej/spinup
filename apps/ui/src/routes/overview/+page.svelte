<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '$lib/api';
  import MetricChart from '$lib/MetricChart.svelte';
  import type { ApplicationSummary, MetricsResponse } from '$lib/types';

  let apps = $state<ApplicationSummary[]>([]);
  let metrics = $state<MetricsResponse | null>(null);
  let metricsErr = $state<string | null>(null);
  let range = $state('15m');

  function stepFor(r: string): string {
    return { '5m': '5s', '15m': '15s', '1h': '30s', '6h': '2m' }[r] ?? '15s';
  }

  async function loadApps() {
    try {
      apps = await api.listApplications();
    } catch {
      /* ignore */
    }
  }
  async function loadMetrics() {
    try {
      metrics = await api.overviewMetrics(range, stepFor(range));
      metricsErr = null;
    } catch (e) {
      metricsErr = (e as Error).message;
    }
  }

  let poll: ReturnType<typeof setInterval> | undefined;
  onMount(() => {
    void loadApps();
    poll = setInterval(() => {
      void loadApps();
      void loadMetrics();
    }, 5000);
  });
  onDestroy(() => {
    if (poll) clearInterval(poll);
  });

  $effect(() => {
    void range;
    void loadMetrics();
  });

  const langCount = $derived.by(() => {
    const c: Record<string, number> = {};
    for (const a of apps) c[a.language] = (c[a.language] ?? 0) + 1;
    return c;
  });
</script>

<div class="flex items-baseline justify-between mb-4">
  <h2 class="m-0">Overview</h2>
  <label class="inline-flex items-center gap-1.5 text-sm text-fg-muted">
    <span>Range</span>
    <select
      bind:value={range}
      class="px-1.5 py-0.5 border border-border-strong rounded text-sm bg-white focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent"
    >
      <option value="5m">5 min</option>
      <option value="15m">15 min</option>
      <option value="1h">1 hour</option>
      <option value="6h">6 hours</option>
    </select>
  </label>
</div>

<section class="card">
  <h3>Platform</h3>
  <div class="flex gap-8 flex-wrap">
    <div>
      <div class="text-xs text-fg-muted uppercase tracking-wider">Applications</div>
      <div class="text-3xl font-semibold font-mono">{apps.length}</div>
    </div>
    {#each Object.entries(langCount) as [lang, n] (lang)}
      <div>
        <div class="text-xs text-fg-muted uppercase tracking-wider">{lang}</div>
        <div class="text-3xl font-semibold font-mono">{n}</div>
      </div>
    {/each}
  </div>
</section>

<section class="card">
  <h3>Rates</h3>
  {#if metricsErr}
    <p class="muted text-sm">Metrics unavailable: {metricsErr}</p>
  {:else}
    <div class="grid grid-cols-[repeat(auto-fit,minmax(300px,1fr))] gap-4 mt-2">
      <MetricChart
        title="HTTP requests / sec"
        points={metrics?.series.httpRequestRate?.points}
        unit="req/s"
        color="#3b82f6"
      />
      <MetricChart
        title="Builds / sec (5m avg)"
        points={metrics?.series.buildRate?.points}
        unit="builds/s"
        color="#059669"
      />
    </div>
  {/if}
</section>
