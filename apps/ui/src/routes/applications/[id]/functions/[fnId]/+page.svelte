<script lang="ts">
  import { page } from '$app/stores';
  import { onMount, onDestroy } from 'svelte';
  import { api } from '$lib/api';
  import { templates, isBuildable, initialFiles } from '$lib/templates';
  import InvokePanel from '$lib/InvokePanel.svelte';
  import CodeEditor from '$lib/CodeEditor.svelte';
  import MetricChart from '$lib/MetricChart.svelte';
  import type { ApplicationDetail, FunctionSummary, MetricsResponse, Source } from '$lib/types';

  const appId = $derived($page.params.id as string);
  const fnId = $derived($page.params.fnId as string);

  let app = $state<ApplicationDetail | null>(null);
  let fn = $state<FunctionSummary | null>(null);
  let err = $state<string | null>(null);

  let files = $state<Record<string, string>>({});
  let activeFile = $state<string>('');
  let sourceLoaded = $state(false);

  let saving = $state(false);
  let saveErr = $state<string | null>(null);

  let editingRoute = $state(false);
  let routeDraft = $state('');
  let routeSaving = $state(false);
  let routeErr = $state<string | null>(null);

  let importing = $state(false);
  let importErr = $state<string | null>(null);

  let runtimeLogText = $state('');
  let runtimeLogErr = $state<string | null>(null);
  let runtimeLogStreaming = $state(false);
  let runtimeLogAbort: AbortController | undefined = undefined;

  let metricsRange = $state('15m');
  let metrics = $state<MetricsResponse | null>(null);
  let metricsPoll: ReturnType<typeof setInterval> | undefined;

  function stepFor(r: string): string {
    return { '5m': '5s', '15m': '15s', '1h': '30s', '6h': '2m' }[r] ?? '15s';
  }
  async function loadMetrics() {
    try {
      metrics = await api.functionMetrics(appId, fnId, metricsRange, stepFor(metricsRange));
    } catch {
      metrics = null;
    }
  }

  async function load() {
    try {
      app = await api.getApplication(appId);
      fn = app.functions.find((f) => f.id === fnId) ?? null;
      if (!fn) throw new Error('function not found in application');
      err = null;
    } catch (e) {
      err = (e as Error).message;
    }
  }
  async function loadSource() {
    try {
      const s = await api.getSource(appId, fnId);
      if (Object.keys(s.files ?? {}).length > 0) {
        files = s.files;
      } else if (app) {
        files = initialFiles(app.language);
      }
      activeFile = Object.keys(files)[0] ?? '';
      sourceLoaded = true;
    } catch {
      sourceLoaded = true;
    }
  }

  onMount(async () => {
    await load();
    void loadSource();
    void loadMetrics();
    metricsPoll = setInterval(() => void loadMetrics(), 10_000);
  });
  onDestroy(() => {
    runtimeLogAbort?.abort();
    if (metricsPoll) clearInterval(metricsPoll);
  });

  $effect(() => {
    void metricsRange;
    void loadMetrics();
  });

  async function streamRuntimeLogs() {
    runtimeLogAbort?.abort();
    runtimeLogAbort = new AbortController();
    runtimeLogText = '';
    runtimeLogErr = null;
    runtimeLogStreaming = true;
    try {
      const res = await fetch(api.applicationLogsUrl(appId, true, 200), {
        signal: runtimeLogAbort.signal
      });
      if (!res.ok || !res.body) {
        runtimeLogErr = (await res.text()) || `HTTP ${res.status}`;
        return;
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        runtimeLogText += decoder.decode(value, { stream: true });
      }
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        runtimeLogErr = (e as Error).message;
      }
    } finally {
      runtimeLogStreaming = false;
    }
  }
  function stopRuntimeLogs() {
    runtimeLogAbort?.abort();
    runtimeLogStreaming = false;
  }

  function startEditRoute() {
    if (!fn) return;
    routeDraft = fn.route;
    routeErr = null;
    editingRoute = true;
  }
  async function saveRoute() {
    if (!fn || routeSaving) return;
    const trimmed = routeDraft.trim();
    if (!trimmed.startsWith('/')) {
      routeErr = 'route must start with "/"';
      return;
    }
    if (trimmed === fn.route) {
      editingRoute = false;
      return;
    }
    routeSaving = true;
    routeErr = null;
    try {
      const updated = await api.updateFunctionRoute(appId, fnId, trimmed);
      fn = updated;
      editingRoute = false;
      void load();
    } catch (e) {
      routeErr = (e as Error).message;
    } finally {
      routeSaving = false;
    }
  }

  async function saveSource() {
    if (saving) return;
    saving = true;
    saveErr = null;
    try {
      await api.putSource(appId, fnId, files);
    } catch (e) {
      saveErr = (e as Error).message;
    } finally {
      saving = false;
    }
  }

  function addFile() {
    const name = prompt('File path (e.g. helper.go or src/util.ts)');
    if (!name) return;
    if (name in files) {
      alert('file exists');
      return;
    }
    files = { ...files, [name]: '' };
    activeFile = name;
  }
  function removeFile(name: string) {
    if (Object.keys(files).length <= 1) {
      alert('cannot remove the last file');
      return;
    }
    if (!confirm(`Delete ${name}?`)) return;
    const next = { ...files };
    delete next[name];
    files = next;
    activeFile = Object.keys(files)[0];
  }

  async function importArchive(e: Event) {
    const input = e.target as HTMLInputElement;
    const f = input.files?.[0];
    if (!f) return;
    importing = true;
    importErr = null;
    try {
      const res = await api.importSource(appId, fnId, f);
      files = res.files ?? {};
      activeFile = Object.keys(files)[0] ?? '';
    } catch (e) {
      importErr = (e as Error).message;
    } finally {
      importing = false;
      input.value = '';
    }
  }

  const primaryFilename = $derived(app ? templates[app.language].filename : '');
  const buildable = $derived(app ? isBuildable(app.language) : false);
</script>

<a class="text-fg-muted no-underline text-sm hover:text-fg" href="/applications/{appId}">← {app?.name ?? 'application'}</a>

{#if err && !fn}
  <p class="error">{err}</p>
{:else if !fn || !app}
  <p class="muted">Loading…</p>
{:else}
  <div class="flex justify-between items-start gap-4 mt-4 mb-6">
    <div>
      <h2 class="mt-0 mb-1.5">{fn.name}</h2>
      <div class="flex gap-1.5 items-center mb-1">
        <span class="pill">{app.language}</span>
        {#if editingRoute}
          <div class="inline-flex items-center gap-1.5">
            <input
              class="font-mono text-xs px-2 py-0.5 border border-border-strong rounded min-w-48 focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent"
              bind:value={routeDraft}
              onkeydown={(e) => { if (e.key === 'Enter') saveRoute(); if (e.key === 'Escape') { editingRoute = false; } }}
            />
            <button class="button primary" onclick={saveRoute} disabled={routeSaving}>
              {routeSaving ? 'Saving…' : 'Save'}
            </button>
            <button class="button" onclick={() => (editingRoute = false)}>Cancel</button>
          </div>
        {:else}
          <code class="font-mono text-xs text-fg-muted bg-bg-elev px-2 py-0.5 rounded-full">{fn.route}</code>
          <button
            class="bg-transparent border-none text-fg-muted font-[inherit] text-xs cursor-pointer underline p-0 hover:text-fg"
            onclick={startEditRoute}
          >edit</button>
        {/if}
      </div>
      {#if routeErr}<p class="error text-xs mt-1">{routeErr}</p>{/if}
      {#if editingRoute}
        <p class="muted text-sm">
          Route changes take effect after the <em>next build</em> — spin.toml is
          baked into the OCI image.
        </p>
      {/if}
      <p class="muted text-sm">Part of application <a href="/applications/{appId}" class="text-fg-muted underline hover:text-fg">{app.name}</a></p>
    </div>
  </div>

  {#if buildable}
    <section class="card">
      <div class="flex justify-between items-baseline">
        <h3>Source</h3>
        <div class="flex gap-1.5">
          <a class="button" href={api.exportSourceUrl(appId, fnId)} download>Export .tar.gz</a>
          <label class="button">
            {importing ? 'Importing…' : 'Import .tar.gz'}
            <input
              type="file"
              accept=".tar.gz,application/gzip"
              onchange={importArchive}
              disabled={importing}
              hidden
            />
          </label>
          <button class="button primary" onclick={saveSource} disabled={saving || !sourceLoaded}>
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
      {#if importErr}<p class="error">{importErr}</p>{/if}
      {#if saveErr}<p class="error">{saveErr}</p>{/if}
      <p class="muted text-sm">
        Primary file for {app.language}: <code class="text-sm bg-bg-elev px-1.5 py-0.5 rounded">{primaryFilename}</code>.
        Add more files (e.g. helpers, <code class="text-sm bg-bg-elev px-1.5 py-0.5 rounded">go.mod</code>, <code class="text-sm bg-bg-elev px-1.5 py-0.5 rounded">package.json</code>) as needed.
      </p>

      <div class="flex gap-0 mt-3 border-b border-border flex-wrap">
        {#each Object.keys(files) as name (name)}
          <button
            class="flex items-center gap-1.5 px-3 py-1.5 mr-0.5 rounded-t border border-border border-b-0 font-mono text-xs cursor-pointer {activeFile === name ? 'bg-white font-semibold' : 'bg-bg-elev'}"
            onclick={() => (activeFile = name)}
          >
            <span>{name}</span>
            {#if Object.keys(files).length > 1}
              <span
                class="text-fg-muted text-base px-0.5 hover:text-danger"
                title="delete file"
                role="button"
                tabindex="-1"
                onclick={(e) => { e.stopPropagation(); removeFile(name); }}
                onkeydown={(e) => { if (e.key === 'Enter') { e.stopPropagation(); removeFile(name); } }}
              >×</span>
            {/if}
          </button>
        {/each}
        <button
          class="flex items-center gap-1.5 px-3 py-1.5 mr-0.5 rounded-t border border-dashed border-border border-b-0 bg-transparent text-fg-muted font-mono text-xs cursor-pointer"
          onclick={addFile}
        >+ file</button>
      </div>

      {#if activeFile}
        <CodeEditor
          filename={activeFile}
          value={files[activeFile]}
          onchange={(v) => (files = { ...files, [activeFile]: v })}
          height={420}
        />
      {/if}
    </section>
  {:else}
    <section class="card muted">
      <p class="text-sm">V1 builder supports Go, JavaScript, TypeScript, and Rust — but <code class="text-sm bg-bg-elev px-1.5 py-0.5 rounded">{app.language}</code> is stubbed out.</p>
    </section>
  {/if}

  <section class="card">
    <h3>Try it out</h3>
    <p class="muted text-sm">
      Requests are relayed by the control plane through the K8s API server's service proxy
      to <code class="text-sm bg-bg-elev px-1.5 py-0.5 rounded">{app.name}.{app.deployment?.namespace ?? '(namespace)'}</code>. Path prefix
      should match this function's route: <code class="text-sm bg-bg-elev px-1.5 py-0.5 rounded">{fn.route}</code>.
    </p>
    {#if !app.deployment}
      <p class="muted text-sm">Application isn't deployed yet — build it first from the app page.</p>
    {:else}
      <InvokePanel appId={appId} fnId={fnId} />
    {/if}
  </section>

  <section class="card">
    <div class="flex justify-between items-baseline">
      <h3>Traffic</h3>
      <label class="inline-flex items-center gap-1.5 text-sm text-fg-muted">
        <span>Range</span>
        <select
          bind:value={metricsRange}
          class="px-1.5 py-0.5 border border-border-strong rounded text-sm bg-white focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent"
        >
          <option value="5m">5 min</option>
          <option value="15m">15 min</option>
          <option value="1h">1 hour</option>
          <option value="6h">6 hours</option>
        </select>
      </label>
    </div>
    <p class="muted text-sm">
      Derived from Spin trigger spans via the OTel collector's spanmetrics
      connector. Populates on the first request after the app is deployed.
    </p>
    <div class="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-4 mt-2">
      <MetricChart title="Request rate" points={metrics?.series.requestRate?.points} unit="req/s" color="#3b82f6" />
      <MetricChart title="p95 latency" points={metrics?.series.latencyP95?.points} unit="ms" color="#059669" />
      <MetricChart title="5xx rate" points={metrics?.series.errorRate?.points} unit="req/s" color="#dc2626" />
    </div>
  </section>

  <section class="card">
    <div class="flex justify-between items-baseline">
      <h3>Runtime logs</h3>
      <div class="flex gap-1.5">
        {#if runtimeLogStreaming}
          <button class="button" onclick={stopRuntimeLogs}>Stop</button>
        {:else}
          <button
            class="button"
            onclick={streamRuntimeLogs}
            disabled={app.runtime === 'workerpool' || !app.deployment}
          >Stream</button>
        {/if}
      </div>
    </div>
    {#if app.runtime === 'workerpool'}
      <p class="muted text-sm">
        Workerpool apps share the spinup-worker pod's stdout — per-function
        streaming isn't split yet. Follow the worker pod for now.
      </p>
    {:else if !app.deployment}
      <p class="muted text-sm">Build + deploy the application to tail pod logs.</p>
    {:else}
      {#if runtimeLogErr}<p class="error">{runtimeLogErr}</p>{/if}
      {#if app.functions.length > 1}
        <p class="muted text-sm">
          This app has {app.functions.length} functions packed into one pod, so
          the stream includes lines from all of them. Look for
          <code class="text-sm bg-bg-elev px-1.5 py-0.5 rounded">component_id=&quot;{fn.name}&quot;</code> to spot Spin trigger
          traces for this function specifically.
        </p>
      {/if}
      <pre class="bg-[#0b1120] text-[#d1d5db] p-3 mt-2 mx-0 mb-0 max-h-[360px] overflow-auto font-mono text-xs whitespace-pre-wrap rounded-md">{runtimeLogText || '(idle — hit Stream to tail the function pod)'}</pre>
    {/if}
  </section>
{/if}
