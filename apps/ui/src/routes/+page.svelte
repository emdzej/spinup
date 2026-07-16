<script lang="ts">
  import { api } from '$lib/api';
  import type { ApplicationSummary } from '$lib/types';

  let apps = $state<ApplicationSummary[]>([]);
  let error = $state<string | null>(null);
  let loading = $state(true);

  async function load() {
    try {
      loading = true;
      error = null;
      apps = await api.listApplications();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void load();
  });
</script>

<div class="flex items-center justify-between gap-4 mb-2">
  <h2 class="m-0">Applications</h2>
  <a class="button primary" href="/applications/new">New application</a>
</div>

<p class="muted mt-0 mb-6 text-[0.95rem]">
  Each application is one <span class="text-accent font-semibold">SpinUP</span>-managed
  SpinApp / pod on the cluster, packing one or more functions with distinct HTTP routes.
</p>

{#if loading}
  <p class="muted">Loading…</p>
{:else if error}
  <p class="error">Failed to load: {error}</p>
{:else if apps.length === 0}
  <div class="text-center px-4 py-12 border border-dashed border-border-strong rounded-lg text-fg-muted">
    <p>No applications yet.</p>
    <a class="button primary mt-4" href="/applications/new">Create your first application</a>
  </div>
{:else}
  <ul class="list-none p-0">
    {#each apps as a (a.id)}
      <li class="border-b border-border">
        <a href="/applications/{a.id}" class="block py-3.5 px-1 text-fg no-underline hover:bg-bg-elev">
          <strong>{a.name}</strong>
          <span class="text-fg-muted text-sm ml-2">{a.language}</span>
          <span class="text-fg-muted text-[0.7rem] ml-2 px-2 py-px border border-border rounded-full uppercase tracking-wider">{a.runtime}</span>
          {#if a.description}<div class="text-fg-muted mt-1 text-sm">{a.description}</div>{/if}
        </a>
      </li>
    {/each}
  </ul>
{/if}
