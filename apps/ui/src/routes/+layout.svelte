<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { fetchMe, loginHref, logout, type Me } from '$lib/auth';

  let { children } = $props();

  // Bootstrap states: loading | error (CP unreachable) | signed-out (401) | ready with Me.
  let me = $state<Me | null>(null);
  let bootstrapped = $state(false);
  let signedOut = $state(false);
  let bootError = $state<string | null>(null);

  onMount(async () => {
    const { status, me: got } = await fetchMe();
    if (status === 401) {
      // Not authenticated — render the splash so the user can start login.
      signedOut = true;
      bootstrapped = true;
      return;
    }
    if (!got) {
      bootError = status === 0 ? 'Cannot reach control plane' : `Control plane error (HTTP ${status})`;
      bootstrapped = true;
      return;
    }
    me = got;
    bootstrapped = true;
  });

  function signIn() {
    const returnTo = window.location.pathname + window.location.search;
    window.location.href = loginHref(returnTo);
  }

  async function onLogout() {
    try {
      const { endSessionUrl } = await logout();
      window.location.href = endSessionUrl || '/';
    } catch (e) {
      console.error(e);
      window.location.reload();
    }
  }
</script>

{#snippet wordmark()}
  <div class="text-4xl tracking-tight font-semibold">
    Spin<span class="text-accent">UP</span>
  </div>
{/snippet}

{#snippet splash(inner: () => any)}
  <div class="min-h-screen flex items-center justify-center bg-bg-elev px-6">
    <div class="card w-full max-w-md text-center py-10 px-8 shadow-sm">
      <div class="mb-6 flex justify-center">
        {@render wordmark()}
      </div>
      {@render inner()}
    </div>
  </div>
{/snippet}

{#if !bootstrapped}
  {#snippet loadingBody()}
    <p class="muted">Loading…</p>
  {/snippet}
  {@render splash(loadingBody)}
{:else if bootError}
  {#snippet errorBody()}
    <h1 class="text-lg font-medium mb-2">Control plane unreachable</h1>
    <p class="muted mb-6">{bootError}. Check that the control plane is running on <code>:8080</code>.</p>
    <button type="button" class="button" onclick={() => window.location.reload()}>Retry</button>
  {/snippet}
  {@render splash(errorBody)}
{:else if signedOut}
  {#snippet loginBody()}
    <p class="muted mb-8">Cloud functions on your cluster.</p>
    <button type="button" class="button primary w-full justify-center py-2" onclick={signIn}>
      Sign in with SSO
    </button>
  {/snippet}
  {@render splash(loginBody)}
{:else if me && !me.authorized}
  {#snippet forbiddenBody()}
    <h1 class="text-lg font-medium mb-2">Not authorized</h1>
    <p class="muted mb-6">
      You're signed in as <strong class="text-fg">{me?.email ?? me?.sub}</strong>, but your account
      is missing the SpinUP role. Ask an administrator to grant it, then sign in again.
    </p>
    <button type="button" class="button" onclick={onLogout}>Sign out</button>
  {/snippet}
  {@render splash(forbiddenBody)}
{:else}
  <header class="flex items-baseline gap-8 px-8 py-4 border-b border-border">
    <a href="/" class="text-fg no-underline text-xl tracking-tight">
      <strong>Spin<span class="text-accent">UP</span></strong>
    </a>
    <nav class="flex gap-4">
      <a href="/" class="text-fg-muted no-underline hover:text-fg">Applications</a>
      <a href="/overview" class="text-fg-muted no-underline hover:text-fg">Overview</a>
    </nav>
    <div class="ml-auto flex items-center gap-3">
      <span class="text-fg-muted text-sm">{me?.email ?? me?.sub}</span>
      <button
        type="button"
        onclick={onLogout}
        class="text-sm text-fg-muted hover:text-fg underline underline-offset-2"
      >
        Logout
      </button>
    </div>
  </header>

  <main class="max-w-[960px] mx-auto p-8">
    {@render children()}
  </main>
{/if}
