<script lang="ts">
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { directoryBrowseURL } from '../../state/routes';
  import { formatCount } from '../../format/decimal';
  import SizeValue from '../../components/SizeValue.svelte';
  import DateValue from '../../components/DateValue.svelte';

  let {
    items,
    selected,
    returnTo,
    select,
  }: {
    items: Result<'GET /api/v1/search/directories'>['items'];
    selected?: string;
    returnTo: string;
    select: (id: string) => void;
  } = $props();
</script>

<table class="data-table directory-search-results">
  <caption>Matching directories — one result per inventory; totals include descendants</caption>
  <thead
    ><tr
      ><th scope="col">Directory and path</th><th scope="col">Disk / inventory</th><th
        scope="col"
        class="numeric">Files</th
      ><th scope="col" class="numeric">Known size</th><th scope="col">Actions</th></tr
    ></thead
  >
  <tbody>
    {#each items as item (item.id)}
      <tr class:active={selected === item.id}>
        <td>
          <a
            href="{resolve('/')}{directoryBrowseURL(
              item.snapshot.id,
              { path: item.directory.path },
              { returnTo },
            ).slice(1)}"
            data-selected-content={selected === item.id ? 'true' : undefined}
            aria-current={selected === item.id ? 'true' : undefined}
            onclick={() => select(item.id)}>{item.directory.path === '' ? 'Root' : item.basename}</a
          >
          <div class="literal muted">
            {item.directory.path === '' ? '(root)' : item.directory.path}
          </div>
        </td>
        <td
          ><span class="record-label">Disk / inventory</span>{item.disk.label}<br />
          <DateValue value={item.snapshot.captured_at} meaning="captured_at" />
          <div>{item.is_current ? 'Current' : 'Historical'} · Inventory {item.snapshot.id}</div>
        </td>
        <td class="numeric"
          ><span class="record-label">Files</span>{formatCount(item.directory.file_count)}</td
        >
        <td class="numeric"
          ><span class="record-label">Known size</span><SizeValue
            value={item.directory.known_bytes}
          />
          {#if !item.directory.size_complete}<div class="muted">
              Partial · {formatCount(item.directory.unknown_size_file_count)} unknown
            </div>{/if}
        </td>
        <td
          ><button
            class="button-compact button-quiet"
            aria-label="Select directory {item.directory.path || 'root'} — inventory {item.snapshot
              .id}"
            aria-pressed={selected === item.id}
            onclick={() => select(item.id)}>Select</button
          ></td
        >
      </tr>
    {/each}
  </tbody>
</table>

<style>
  .active {
    background: var(--color-raised);
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  td {
    overflow-wrap: anywhere;
  }
  td > a {
    font-weight: 600;
  }
  .record-label {
    display: none;
  }
  @media (max-width: 900px) {
    table,
    tbody {
      display: block;
    }
    thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }
    tr {
      display: grid;
      grid-template-columns: 1fr 1fr;
      border-block-end: 1px solid var(--color-border);
      padding-block: var(--space-2);
    }
    .data-table td {
      display: block;
      height: auto;
      border: 0;
      min-inline-size: 0;
      text-align: left;
    }
    td:first-child,
    td:last-child {
      grid-column: 1 / -1;
    }
    .record-label {
      display: block;
      color: var(--color-muted);
      font-weight: 600;
    }
    caption {
      display: block;
      inline-size: 100%;
    }
  }
</style>
