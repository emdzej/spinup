<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import uPlot from 'uplot';
  import 'uplot/dist/uPlot.min.css';
  import type { Point } from './types';

  interface Props {
    title: string;
    points: Point[] | null | undefined;
    unit?: string;
    formatValue?: (v: number) => string;
    color?: string;
  }

  let { title, points, unit = '', formatValue, color = '#3b82f6' }: Props = $props();

  let container: HTMLDivElement | undefined = $state();
  let chart: uPlot | undefined;

  function toData(pts: Point[] | null | undefined): uPlot.AlignedData {
    if (!pts || pts.length === 0) return [[], []];
    return [pts.map((p) => p[0]), pts.map((p) => p[1])];
  }

  const defaultFmt = (v: number): string => {
    if (unit === 'bytes') {
      const units = ['B', 'KB', 'MB', 'GB'];
      let n = v;
      let i = 0;
      while (n >= 1024 && i < units.length - 1) {
        n /= 1024;
        i++;
      }
      return `${n.toFixed(1)} ${units[i]}`;
    }
    if (unit === 'cores') return `${v.toFixed(3)}`;
    if (unit === 'req/s' || unit === 'builds/s') return `${v.toFixed(2)}/s`;
    return v.toFixed(2);
  };

  function build() {
    if (!container) return;
    const width = container.clientWidth || 300;
    const opts: uPlot.Options = {
      width,
      height: 120,
      cursor: { show: true },
      legend: { show: false },
      scales: { x: { time: true } },
      axes: [
        { stroke: '#9ca3af', grid: { stroke: '#f3f4f6' } },
        {
          stroke: '#9ca3af',
          grid: { stroke: '#f3f4f6' },
          values: (_, ticks) => ticks.map((t) => (formatValue ?? defaultFmt)(t))
        }
      ],
      series: [
        {},
        {
          stroke: color,
          fill: color + '22',
          width: 2,
          points: { show: false }
        }
      ]
    };
    chart = new uPlot(opts, toData(points), container);
  }

  onMount(() => {
    build();
    const obs = new ResizeObserver(() => {
      if (chart && container) chart.setSize({ width: container.clientWidth, height: 120 });
    });
    if (container) obs.observe(container);
    return () => obs.disconnect();
  });

  onDestroy(() => {
    chart?.destroy();
  });

  $effect(() => {
    if (chart) chart.setData(toData(points));
  });

  const latest = $derived(points && points.length > 0 ? points[points.length - 1][1] : null);
</script>

<div class="flex flex-col">
  <div class="flex items-baseline justify-between mb-1">
    <span class="text-sm text-fg-muted">{title}</span>
    {#if latest !== null}
      <span class="font-mono text-sm font-semibold">{(formatValue ?? defaultFmt)(latest)}</span>
    {:else}
      <span class="font-mono text-sm text-fg-muted font-normal">—</span>
    {/if}
  </div>
  <div class="w-full min-h-[120px]" bind:this={container}></div>
</div>
