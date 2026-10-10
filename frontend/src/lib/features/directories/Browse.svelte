<script lang="ts">
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { useConnection } from '../../state/context';
  import { directoryDetail, directoryPages } from '../../state/feature-requests';
  import {
    parseDirectory,
    directoryBrowseURL,
    routes,
    type DirectoryRoute,
  } from '../../state/routes';
  import type { RequestState } from '../../state/request';
  import type { TraversalState } from '../../state/traversal';
  import { breadcrumbs } from '../../format/path';
  import { formatCount } from '../../format/decimal';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import PageControls from '../../components/PageControls.svelte';
  import DateValue from '../../components/DateValue.svelte';
  import Sizes from './Sizes.svelte';
  import Entries from './Entries.svelte';
  import Filters from './Filters.svelte';
  import Replicas from './Replicas.svelte';
  import Coverage from './Coverage.svelte';
  let { id, search }: { id: string; search: string } = $props();
  const connection = useConnection();
  type Detail = Result<'GET /api/v1/snapshots/{id}/directory'>;
  type Page = Result<'GET /api/v1/snapshots/{id}/directory/entries'>;
  let query = $state<DirectoryRoute>();
  let parseError = $state('');
  let detailOwner = $state<ReturnType<typeof directoryDetail>>();
  let pagesOwner = $state<ReturnType<typeof directoryPages>>();
  let detail = $state<RequestState<Detail>>({ status: 'idle' });
  let view = $state<TraversalState<Page>>({ status: 'idle', pages: [], active: 0, paged: false });
  let items = $state<Page['items']>([]);
  let draft = $state(false);
  let tab = $state<'entries' | 'replicas' | 'coverage'>('entries');
  let paging = $state({ busy: false, canMore: false, canNext: false, canPrevious: false });
  const source = $derived(detail.status === 'success' ? detail.value : view.pages[0]?.value);
  $effect(() => {
    let input: DirectoryRoute;
    try {
      input = parseDirectory(new globalThis.URLSearchParams(search));
    } catch (error) {
      parseError = error instanceof Error ? error.message : 'Invalid directory route';
      return;
    }
    query = input;
    const summary = directoryDetail(connection, id, input.path);
    const pages = directoryPages(connection, id, input);
    detailOwner = summary;
    pagesOwner = pages;
    const unDetail = summary.request.subscribe((state) => {
      detail = state;
    });
    const unPages = pages.traversal.subscribe((state) => {
      view = state;
      items = pages.traversal.items;
      paging = {
        busy: pages.traversal.busy,
        canMore: pages.traversal.canMore,
        canNext: pages.traversal.canNext,
        canPrevious: pages.traversal.canPrevious,
      };
    });
    void summary.reload();
    void pages.traversal.restart();
    return () => {
      unDetail();
      unPages();
      summary.dispose();
      pages.dispose();
    };
  });
  function retire() {
    draft = true;
    pagesOwner?.traversal.retire();
  }
  function resume() {
    draft = false;
    void pagesOwner?.traversal.restart();
  }
  function retry() {
    const traversal = pagesOwner?.traversal;
    if (!traversal) return;
    const disconnected =
      view.error instanceof ApiError &&
      (view.error.kind === 'transport' || view.error.status === 503);
    if (!view.pages.length || view.status === 'stale' || disconnected) void traversal.restart();
    else if (view.paged || view.pages.length >= 10) void traversal.next();
    else void traversal.more();
  }
</script>

<h1>Directory browser</h1>
{#if parseError}<p role="alert">{parseError}</p>
  <pre>{search}</pre>
{:else if query}
  <p><a href="{resolve('/')}{routes.snapshot(id).slice(1)}">Inventory {id}</a></p>
  <nav aria-label="Directory breadcrumbs">
    <a href="{resolve('/')}{directoryBrowseURL(id, { ...query, path: '' }).slice(1)}"
      >Root{source ? ` — Disk ${source.snapshot.disk_id}` : ` — Inventory ${id}`}</a
    >
    {#each breadcrumbs(query.path!) as crumb (crumb.path)}
      <span> / </span>
      {#if crumb.path === query.path}<span class="literal" aria-current="page">{crumb.name}</span>
      {:else}<a
          class="literal"
          href="{resolve('/')}{directoryBrowseURL(id, { ...query, path: crumb.path }).slice(1)}"
          >{crumb.name}</a
        >{/if}
    {/each}
  </nav>
  <section aria-label="Directory summary">
    <h2>Directory summary</h2>
    <RequestFeedback
      status={detail.status}
      error={detail.status === 'failure' ? detail.error : undefined}
      resource="Directory summary"
      retry={() => void detailOwner?.reload()}
    />
    <button onclick={() => void detailOwner?.reload()}>Reload summary</button>
    {#if detail.status === 'idle'}<p>
        Summary invalidated; reload to verify current redundancy.
      </p>{/if}
    {#if detail.status === 'success'}
      <p>
        {detail.value.is_current ? 'Current source inventory' : 'Historical source inventory'} —
        <a href="{resolve('/')}{routes.disk(detail.value.snapshot.disk_id).slice(1)}"
          >Disk {detail.value.snapshot.disk_id}</a
        >
      </p>
      <DateValue value={detail.value.snapshot.captured_at} meaning="captured_at" />
      <Sizes item={detail.value.directory} />
      <section aria-label="Current other-disk redundancy">
        <h3>Current other-disk redundancy</h3>
        <p>
          All descendant file occurrences, against current destination inventories only; the source
          disk is excluded. Historical snapshots do not add replicas.
        </p>
        <ul>
          {#each detail.value.redundancy_histogram as bucket (bucket.other_disk_count)}<li>
              {formatCount(bucket.other_disk_count)} other disks: {formatCount(bucket.file_count)} files
            </li>{/each}
        </ul>
        {#if !detail.value.redundancy_histogram.length}<p>No descendant files.</p>{/if}
      </section>
    {/if}
  </section>
  <nav aria-label="Directory views">
    <button aria-pressed={tab === 'entries'} onclick={() => (tab = 'entries')}>Entries</button>
    <button aria-pressed={tab === 'replicas'} onclick={() => (tab = 'replicas')}
      >Exact tree copies</button
    >
    <button aria-pressed={tab === 'coverage'} onclick={() => (tab = 'coverage')}
      >Content on other disks</button
    >
  </nav>
  {#if tab === 'replicas'}
    {#key `${id}:${query.path}`}<Replicas {id} path={query.path!} />{/key}
  {:else if tab === 'coverage'}
    {#key `${id}:${query.path}`}<Coverage {id} path={query.path!} />{/key}
  {:else}
    <section aria-label="Directory entry listing">
      <h2>Entries</h2>
      <Filters {id} {query} {retire} {resume} />
      {#if draft}<p role="status">Filters changed; apply to load entries.</p>
      {:else}
        <RequestFeedback
          status={view.status}
          error={view.error}
          resource="Directory entries"
          {retry}
        />
        {#if view.status !== 'stale'}<button
            disabled={paging.busy}
            onclick={() => void pagesOwner?.traversal.restart()}>Reload entries</button
          >{/if}
        {#if view.status === 'idle'}<p>Entries invalidated; reload results.</p>{/if}
        {#if view.pages.length}
          {#if view.error}<p>Previously loaded entries; not freshly verified.</p>{/if}
          <p>
            {view.pages[0]!.value.is_current
              ? 'Current source inventory'
              : 'Historical source inventory'}. Other replica counts refer to current destination
            inventories, not historical copies.
          </p>
          {#if items.length}<Entries
              {items}
              {id}
              {query}
              returnTo={directoryBrowseURL(id, query)}
            />
          {:else}<p>
              No entries match this listing. This does not imply an empty recursive summary.
            </p>{/if}
          <PageControls
            {...paging}
            paged={view.paged}
            page={view.active}
            firstRetained={view.pages[0]!.number}
            loaded={items.length}
            complete={view.pages.at(-1)!.value.next_cursor === null}
            more={() => void pagesOwner?.traversal.more()}
            next={() => void pagesOwner?.traversal.next()}
            previous={() => pagesOwner?.traversal.previous()}
          />
        {/if}
      {/if}
    </section>
  {/if}
{/if}

<style>
  .literal {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  nav {
    margin-block: 1rem;
  }
</style>
