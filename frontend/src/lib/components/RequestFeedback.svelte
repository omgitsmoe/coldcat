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
    retryLabel = 'Retry',
    staleLabel = 'Inventory changed; reload results',
    staleActionLabel = 'Reload results',
    invalidatedLabel,
    emptyLabel,
    validationLabel,
    retainedLabel = 'Previously loaded results; not freshly verified.',
    retained = false,
  }: {
    status: string;
    error?: unknown;
    resource?: string;
    retry?: () => void;
    loadingLabel?: string;
    retryLabel?: string;
    staleLabel?: string;
    staleActionLabel?: string;
    invalidatedLabel?: string;
    emptyLabel?: string;
    validationLabel?: string;
    retainedLabel?: string;
    retained?: boolean;
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
  <p class="request-feedback loading" role="status">
    {loadingLabel}{#if extended}
      Still working; you can keep navigating.{/if}
  </p>
{:else if status === 'validation' && validationLabel}
  <p class="request-feedback warning" role="alert">{validationLabel}</p>
{:else if status === 'stale'}
  <div class="request-feedback warning" role="alert">
    <p>{staleLabel}</p>
    {#if retry}<button type="button" onclick={retry}>{staleActionLabel}</button>{/if}
  </div>
{:else if status === 'failure'}
  <div class="request-feedback failure" role="alert">
    <p>
      {message.title}{#if message.detail}: {message.detail}{/if}
    </p>
    {#if retained}<p>{retainedLabel}</p>{/if}
    {#if retry && !uncertainWrite}<button type="button" onclick={retry}>{retryLabel}</button>{/if}
  </div>
{:else if status === 'idle' && invalidatedLabel}
  <div class="request-feedback warning" role="status"><p>{invalidatedLabel}</p></div>
{:else if status === 'success' && emptyLabel}
  <p class="request-feedback empty" role="status">{emptyLabel}</p>
{/if}

<style>
  .request-feedback {
    margin-block: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--color-border);
    background: var(--color-surface);
    overflow-wrap: anywhere;
  }
  .request-feedback p {
    margin: 0;
  }
  .request-feedback button {
    margin-block-start: var(--space-2);
  }
  .warning {
    border-color: var(--color-warning);
  }
  .failure {
    border-color: var(--color-error);
  }
</style>
