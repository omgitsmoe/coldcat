<script lang="ts">
  import type { components } from '../../api/generated/wire';
  import { formatBytes, formatCount } from '../../format/decimal';
  import DateValue from '../../components/DateValue.svelte';
  let { item }: { item: components['schemas']['DirectorySummary'] } = $props();
</script>

<section aria-label="Recursive sizes">
  <h3>Recursive sizes</h3>
  <dl>
    <dt>Descendant files / distinct contents</dt>
    <dd>{formatCount(item.file_count)} / {formatCount(item.content_count)}</dd>
    <dt>Recursive file bytes (each path counts)</dt>
    <dd>
      {formatBytes(item.size_complete ? item.known_bytes : null)} — {item.size_complete
        ? 'Complete'
        : 'Incomplete'}
    </dd>
    <dt>Known file-byte subtotal / unknown-size files</dt>
    <dd>{formatBytes(item.known_bytes)} / {formatCount(item.unknown_size_file_count)}</dd>
    <dt>Unique-content bytes (each identity counts once)</dt>
    <dd>
      {formatBytes(item.unique_content_size_complete ? item.unique_content_known_bytes : null)} — {item.unique_content_size_complete
        ? 'Complete'
        : 'Incomplete'}
    </dd>
    <dt>Known unique-content subtotal / unknown-size contents</dt>
    <dd>
      {formatBytes(item.unique_content_known_bytes)} / {formatCount(
        item.unknown_size_content_count,
      )}
    </dd>
  </dl>
  <p>
    Cataloged logical bytes, not filesystem allocation or free space. Sizes include all descendant
    files, independent of entry filters.
  </p>
  <DateValue value={item.max_known_mtime} meaning="mtime" />
</section>
