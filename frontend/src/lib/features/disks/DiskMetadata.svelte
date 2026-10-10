<script lang="ts">
  import type { Result } from '../../api/client';
  import { formatBytes } from '../../format/decimal';
  let { item }: { item: Result<'GET /api/v1/disks'>['items'][number] } = $props();
</script>

<dl>
  <dt>Label</dt>
  <dd class="literal">{item.label}</dd>
  <dt>Declared capacity</dt>
  <dd>{formatBytes(item.capacity)}</dd>
  <dt>Serial</dt>
  <dd class="literal">{item.serial ?? 'Unknown'}</dd>
  <dt>Notes</dt>
  <dd class="literal">{item.notes ?? 'None'}</dd>
</dl>

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
