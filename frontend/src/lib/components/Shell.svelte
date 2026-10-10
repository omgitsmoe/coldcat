<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  import { createClient } from '../api/client';
  import { Connection, type ConnectionState } from '../state/connection';
  import { provideConnection } from '../state/context';
  let {
    children,
    brand,
    navigation,
  }: {
    children?: Snippet;
    brand?: Snippet;
    navigation?: Snippet;
  } = $props();
  const connection = new Connection(createClient());
  provideConnection(connection);
  let state = $state<ConnectionState>({ status: 'checking' });

  onMount(() => {
    const unsubscribe = connection.subscribe((value) => {
      state = value;
    });
    void connection.check();
    return () => {
      unsubscribe();
      connection.dispose();
    };
  });
</script>

<a class="skip-link" href="#main">Skip to main content</a>
<header>
  {@render brand?.()}
  {@render navigation?.()}
  <p role="status" aria-live="polite">
    {#if state.status === 'checking'}Checking backend…
    {:else if state.status === 'ready'}Backend ready
    {:else}Backend unavailable. Start the local server, then retry.{/if}
  </p>
  {#if state.status === 'unavailable'}
    <button onclick={() => connection.retry()}>Retry connection</button>
  {/if}
</header>
<main id="main" tabindex="-1">{@render children?.()}</main>
