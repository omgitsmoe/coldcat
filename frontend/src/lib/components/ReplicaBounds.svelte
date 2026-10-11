<script lang="ts">
  import type { BoundsMode } from '../features/search/filters';

  let {
    mode = $bindable(),
    exact = $bindable(),
    minimum = $bindable(),
    maximum = $bindable(),
    invalid = false,
  }: {
    mode: BoundsMode;
    exact: string;
    minimum: string;
    maximum: string;
    invalid?: boolean;
  } = $props();
</script>

<div class="control-group">
  <label class="field"
    >Other-copy matching
    <select bind:value={mode} aria-invalid={invalid}>
      <option value="any">Any</option>
      <option value="exact">Exactly</option>
      <option value="range">Range</option>
      {#if mode === 'conflict'}<option value="conflict">Conflicting URL bounds</option>{/if}
    </select>
  </label>
  {#if mode === 'exact' || mode === 'conflict'}
    <label class="field"
      >Exact other copies
      <input bind:value={exact} inputmode="numeric" aria-invalid={invalid} />
    </label>
  {/if}
  {#if mode === 'range' || mode === 'conflict'}
    <label class="field"
      >Minimum other copies
      <input bind:value={minimum} inputmode="numeric" aria-invalid={invalid} />
    </label>
    <label class="field"
      >Maximum other copies
      <input bind:value={maximum} inputmode="numeric" aria-invalid={invalid} />
    </label>
  {/if}
</div>

<style>
  input {
    inline-size: 9rem;
  }
</style>
