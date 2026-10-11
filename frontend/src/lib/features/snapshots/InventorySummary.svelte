<script lang="ts">
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { routes, directoryBrowseURL, type DetailContext } from '../../state/routes';
  import { formatCount } from '../../format/decimal';
  import DateValue from '../../components/DateValue.svelte';
  let {
    item,
    context = {},
    provenance = false,
  }: {
    item: Result<'GET /api/v1/snapshots/{id}'>;
    context?: DetailContext;
    provenance?: boolean;
  } = $props();
</script>

<a href="{resolve('/')}{routes.snapshot(item.id, context).slice(1)}">Inventory {item.id}</a>
<DateValue value={item.captured_at} meaning="captured_at" />
{#if !provenance}<DateValue value={item.imported_at} meaning="imported_at" />{/if}
<dl class="metrics">
  <div class="metric">
    <dt>Files</dt>
    <dd>{formatCount(item.file_count)}</dd>
  </div>
  <div class="metric">
    <dt>Distinct contents</dt>
    <dd>{formatCount(item.content_count)}</dd>
  </div>
</dl>
<a
  class="button button-primary"
  href="{resolve('/')}{directoryBrowseURL(item.id, { path: '' }).slice(1)}">Browse root</a
>
{#if provenance}
  <details>
    <summary>Import and capture provenance</summary>
    <DateValue value={item.imported_at} meaning="imported_at" />
    <dl>
      <dt>State</dt>
      <dd>{item.state}</dd>
      <dt>Capture provenance</dt>
      <dd>
        {item.capture_provenance === 'explicit'
          ? 'Explicit capture time'
          : 'Source inventory file modification time'}
      </dd>
      <dt>Source input path (backend provenance)</dt>
      <dd class="literal">{item.input_path}</dd>
      <dt>Source input format</dt>
      <dd class="literal">{item.input_format}</dd>
    </dl>
    <p>Source paths describe backend input files, not browser file links.</p>
    <p>
      Captured at describes the source inventory's capture time; Imported at is when the catalog
      received it. Capture age is not proof an offline disk was recently checked.
    </p>
  </details>
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
    margin: 0 0 0.5rem;
  }
</style>
