<script lang="ts">
  import type { components } from '../../api/generated/wire';
  import { formatCount } from '../../format/decimal';
  import SizeValue from '../../components/SizeValue.svelte';
  let { item }: { item: components['schemas']['DirectorySummary'] } = $props();
</script>

<section aria-label="Recursive sizes">
  <p class="summary-count">
    All descendants (unfiltered): {formatCount(item.file_count)} files / {formatCount(
      item.content_count,
    )} distinct contents
  </p>
  <div class="sizes">
    <dl class="metric">
      <dt>Recursive file bytes (each path counts)</dt>
      <dd>
        <SizeValue value={item.size_complete ? item.known_bytes : null} /> — {item.size_complete
          ? 'Complete'
          : 'Incomplete'}
      </dd>
      {#if !item.size_complete}<dd class="muted">
          Known file-byte subtotal: <SizeValue value={item.known_bytes} />; {formatCount(
            item.unknown_size_file_count,
          )} unknown-size files
        </dd>{/if}
    </dl>
    <dl class="metric">
      <dt>Unique-content bytes (each identity counts once)</dt>
      <dd>
        <SizeValue
          value={item.unique_content_size_complete ? item.unique_content_known_bytes : null}
        /> — {item.unique_content_size_complete ? 'Complete' : 'Incomplete'}
      </dd>
      {#if !item.unique_content_size_complete}<dd class="muted">
          Known unique-content subtotal: <SizeValue value={item.unique_content_known_bytes} />; {formatCount(
            item.unknown_size_content_count,
          )} unknown-size contents
        </dd>{/if}
    </dl>
  </div>
</section>

<style>
  .sizes {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
  }
  .summary-count {
    margin-block: var(--space-2);
  }
  dl {
    margin: 0;
  }
  .metric dd.muted {
    font-size: var(--text-small);
    line-height: var(--line-ui);
  }
  @media (max-width: 600px) {
    .sizes {
      grid-template-columns: 1fr;
    }
  }
</style>
