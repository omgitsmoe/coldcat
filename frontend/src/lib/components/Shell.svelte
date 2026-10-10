<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  let { children, brand }: { children?: Snippet; brand?: Snippet } = $props();
  let state = $state<'checking' | 'ready' | 'unavailable'>('checking');

  async function checkReadiness() {
    state = 'checking';
    try {
      const response = await fetch('/healthz');
      if (!response.ok) throw new Error('Backend readiness failed');
      const payload: unknown = await response.json();
      if (
        typeof payload !== 'object' ||
        payload === null ||
        !('status' in payload) ||
        payload.status !== 'ready'
      )
        throw new Error('Unexpected readiness response');
      state = 'ready';
    } catch {
      state = 'unavailable';
    }
  }

  onMount(() => {
    void checkReadiness();
  });
</script>

<a class="skip-link" href="#main">Skip to main content</a>
<header>
  {@render brand?.()}
  <p role="status" aria-live="polite">
    {#if state === 'checking'}Checking backend…
    {:else if state === 'ready'}Backend ready
    {:else}Backend unavailable. Start the local server, then retry.{/if}
  </p>
  {#if state === 'unavailable'}
    <button onclick={checkReadiness}>Retry connection</button>
  {/if}
</header>
<main id="main" tabindex="-1">{@render children?.()}</main>
