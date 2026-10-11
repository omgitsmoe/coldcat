<script lang="ts">
  import type { Rules } from './rules';
  let { applied, dirty, limit }: { applied?: Rules; dirty: boolean; limit: string } = $props();
</script>

{#if dirty}
  <p role="status" class="status-warning">Rules changed; apply to compare.</p>
  <p class="muted">
    Unapplied changes — previous results retired. Draft rules and page size need explicit execution.
  </p>
{/if}
{#if applied}
  <details class="applied-rules">
    <summary
      >{dirty ? 'Last applied rules' : 'Applied rules'}: {applied.allow.length} allow · {applied
        .block.length} block · page size {limit}</summary
    >
    <dl>
      {#each ['allow', 'block'] as kind (kind)}
        {@const key = kind as keyof Rules}
        <dt>{key === 'allow' ? 'Allow' : 'Block'}</dt>
        <dd>
          {#if applied[key].length}
            <ul>
              {#each applied[key] as row, index (index)}<li class="literal">{row}</li>{/each}
            </ul>
          {:else}None{/if}
        </dd>
      {/each}
    </dl>
  </details>
{/if}

<style>
  .applied-rules {
    margin-block: var(--space-3);
  }
  dl {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--space-2);
  }
  dd,
  ul {
    margin: 0;
  }
</style>
