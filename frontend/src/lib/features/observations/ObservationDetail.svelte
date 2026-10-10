<script lang="ts">
  import { resolve } from '$app/paths';
  import { useConnection } from '../../state/context';
  import { observationDetail } from '../../state/feature-requests';
  import { parseDetailContext, contentURL, routes, directoryBrowseURL } from '../../state/routes';
  import type { RequestState } from '../../state/request';
  import type { Result } from '../../api/client';
  import { containingDirectory } from '../../format/path';
  import { formatBytes, formatCount } from '../../format/decimal';
  import DateValue from '../../components/DateValue.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  let { id, search }: { id: string; search: string } = $props();
  const connection = useConnection();
  type Observation = Result<'GET /api/v1/observations/{id}'>;
  let context = $state<ReturnType<typeof parseDetailContext>>();
  let parseError = $state('');
  let owner = $state<ReturnType<typeof observationDetail>>();
  let view = $state<RequestState<Observation>>({ status: 'idle' });
  let cached = $state<Observation>();
  const item = $derived(view.status === 'success' ? view.value : cached);
  $effect(() => {
    try {
      context = parseDetailContext(new globalThis.URLSearchParams(search));
    } catch (error) {
      parseError = error instanceof Error ? error.message : 'Invalid observation route';
      return;
    }
    const detail = observationDetail(connection, id);
    owner = detail;
    const unsubscribe = detail.request.subscribe((value) => {
      view = value;
      if (value.status === 'success') cached = value.value;
    });
    const unbindCache = connection.onInvalidate((reason) => {
      if (reason !== 'disconnect') cached = undefined;
    });
    void detail.reload();
    return () => {
      unsubscribe();
      unbindCache();
      detail.dispose();
    };
  });
</script>

<h1>Observation {id}</h1>
{#if parseError}<p role="alert">{parseError}</p>
  <pre>{search}</pre>
{:else if context}
  {#if context.returnTo}<p>
      <a href="{resolve('/')}{context.returnTo.slice(1)}"
        >{context.returnTo.startsWith('/snapshots/')
          ? 'Return to directory'
          : 'Return to search'}</a
      >
    </p>{/if}
  <RequestFeedback
    status={view.status}
    error={view.status === 'failure' ? view.error : undefined}
    resource="Observation"
    retry={() => void owner?.reload()}
  />
  {#if view.status === 'idle'}<p role="status">
      Observation data invalidated; not freshly verified.
      <button onclick={() => void owner?.reload()}>Reload observation</button>
    </p>{/if}
  {#if item}
    {#if view.status !== 'success'}<p>Previously loaded observation; not freshly verified.</p>{/if}
    <h2>Disk-relative path</h2>
    <div class="literal">{item.observation.path}</div>
    <CopyButton text={item.observation.path} label="Copy path" />
    <dl>
      <dt>Content</dt>
      <dd>
        <a
          href="{resolve('/')}{contentURL(
            item.content.id,
            { scope: 'current' },
            { ...context, observation: context.observation ?? id },
          ).slice(1)}">Content {item.content.id}</a
        >
      </dd>
      <dt>Algorithm</dt>
      <dd>{item.content.hash.algorithm}</dd>
      <dt>Digest</dt>
      <dd>
        <code class="literal">{item.content.hash.hex}</code><CopyButton
          text={item.content.hash.hex}
          label="Copy digest"
        />
      </dd>
      <dt>Size</dt>
      <dd>{formatBytes(item.content.size)}</dd>
      <dt>Disk</dt>
      <dd>
        <a href="{resolve('/')}{routes.disk(item.disk.id, context).slice(1)}"
          >Disk {item.disk.label}</a
        >
      </dd>
      <dt>Inventory</dt>
      <dd>
        <a href="{resolve('/')}{routes.snapshot(item.snapshot.id, context).slice(1)}"
          >Inventory {item.snapshot.id}</a
        >
      </dd>
      <dt>Other current locations</dt>
      <dd>{formatCount(item.other_location_count)}</dd>
      <dt>Other current disks</dt>
      <dd>{formatCount(item.other_disk_count)}</dd>
    </dl>
    <p>
      Other counts exclude this observation's disk/path or disk only when currently present for this
      content. Historical-only content has zero current copies.
    </p>
    <h2>Dates</h2>
    <DateValue value={item.observation.mtime} meaning="mtime" />
    <DateValue value={item.snapshot.captured_at} meaning="captured_at" />
    <DateValue value={item.snapshot.imported_at} meaning="imported_at" />
    <p>Capture dates describe inventory age, not when an offline disk was last checked.</p>
    <a
      href="{resolve('/')}{directoryBrowseURL(item.snapshot.id, {
        path: containingDirectory(item.observation.path),
      }).slice(1)}">Containing directory</a
    >
  {/if}
{/if}

<style>
  .literal {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  dt {
    font-weight: bold;
  }
  dd {
    margin: 0 0 0.75rem;
  }
</style>
