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
  } = $props();
</script>

<nav aria-label="Result pages">
  {#if paged}
    <p>Page {page}. Earlier pages retained from page {firstRetained}.</p>
    <button disabled={busy || !canPrevious} onclick={previous}>Previous page</button>
    <button disabled={busy || !canNext} onclick={next}>Next page</button>
  {:else}
    <p>{loaded} loaded</p>
    {#if busy}
      <button disabled>Loading page…</button>
    {:else if canMore}
      <button disabled={busy} onclick={more}>Load more</button>
    {:else if canNext}
      <p>Retained-page limit reached. Continue one page at a time; earlier pages are limited.</p>
      <button disabled={busy || !canNext} onclick={next}>Next page</button>
    {:else if complete}
      <p>All results loaded.</p>
    {:else}
      <p>Continuation unavailable; retry the connection or reload results.</p>
    {/if}
  {/if}
</nav>
