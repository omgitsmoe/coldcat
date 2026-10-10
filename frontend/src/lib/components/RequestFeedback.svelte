<script lang="ts">
  import { onDestroy } from 'svelte';
  import { errorPresentation } from '../state/presentation';
  import { ApiError } from '../api/errors';
  let {
    status,
    error,
    resource = 'Resource',
    retry,
    loadingLabel = 'Loading…',
  }: {
    status: string;
    error?: unknown;
    resource?: string;
    retry?: () => void;
    loadingLabel?: string;
  } = $props();
  let extended = $state(false);
  let timer: ReturnType<typeof globalThis.setTimeout> | undefined;
  $effect(() => {
    globalThis.clearTimeout(timer);
    extended = false;
    if (status === 'loading' || status === 'loading-more') {
      timer = globalThis.setTimeout(() => {
        extended = true;
      }, 1000);
    }
    return () => globalThis.clearTimeout(timer);
  });
  onDestroy(() => globalThis.clearTimeout(timer));
  const message = $derived(errorPresentation(error, resource));
  const uncertainWrite = $derived(error instanceof ApiError && error.uncertainWrite);
</script>

{#if status === 'loading' || status === 'loading-more'}
  <p role="status">
    {loadingLabel}{#if extended}
      Still working; you can keep navigating.{/if}
  </p>
{:else if status === 'stale'}
  <div role="alert">
    <p>Inventory changed; reload results</p>
    {#if retry}<button onclick={retry}>Reload results</button>{/if}
  </div>
{:else if status === 'failure'}
  <div role="alert">
    <p>
      {message.title}{#if message.detail}: {message.detail}{/if}
    </p>
    {#if retry && !uncertainWrite}<button onclick={retry}>Retry</button>{/if}
  </div>
{/if}
