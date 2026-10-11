<script lang="ts">
  let {
    busy,
    canMore,
    canNext,
    canPrevious,
    paged,
    page,
    firstRetained,
    loaded,
    complete,
    more,
    next,
    previous,
    reload,
    reloadLabel = 'Reload results',
  }: {
    busy: boolean;
    canMore: boolean;
    canNext: boolean;
    canPrevious: boolean;
    paged: boolean;
    page: number;
    firstRetained: number;
    loaded: number;
    complete: boolean;
    more: () => void;
    next: () => void;
    previous: () => void;
    reload?: () => void;
    reloadLabel?: string;
  } = $props();
  let pendingAction = $state<'more' | 'next'>('more');
  $effect(() => {
    if (!busy && !paged) pendingAction = canMore ? 'more' : canNext ? 'next' : 'more';
  });
</script>

<nav class="page-controls" aria-label="Result pages" aria-busy={busy}>
  {#if paged}
    <p role="status">
      Page {page}. {loaded} loaded on this page. Earlier pages retained from page {firstRetained}.
    </p>
    <button type="button" disabled={busy || !canPrevious} onclick={previous}>Previous page</button>
    <button type="button" disabled={busy || !canNext} onclick={next}>Next page</button>
  {:else}
    <p role="status">{loaded} loaded</p>
    {#if busy}
      <button type="button" class:page-load={pendingAction === 'more'} disabled
        >{pendingAction === 'next' ? 'Next page' : 'Load more'}</button
      >
    {:else if canMore}
      <button type="button" class="page-load" disabled={busy} onclick={more}>Load more</button>
    {:else if canNext}
      <p>Retained-page limit reached. Next shows one page at a time; older pages expire.</p>
      <button type="button" disabled={busy || !canNext} onclick={next}>Next page</button>
    {:else if complete}
      <p>All results loaded.</p>
    {:else}
      <p>Continuation unavailable; retry the connection or reload results.</p>
    {/if}
  {/if}
  {#if reload}<button
      type="button"
      class="button-quiet page-reload"
      disabled={busy}
      onclick={reload}>{reloadLabel}</button
    >{/if}
</nav>

<style>
  .page-controls {
    align-items: center;
  }
  p {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
  .page-reload {
    margin-inline-start: auto;
  }
</style>
