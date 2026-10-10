<script lang="ts">
  import '../app.css';
  import { resolve } from '$app/paths';
  import Shell from '../lib/components/Shell.svelte';
  let { children } = $props();
</script>

<Shell>
  {#snippet brand()}<a class="brand" href={resolve('/')}>Coldcat</a>{/snippet}
  {#snippet navigation()}
    <nav aria-label="Main navigation">
      <a href={resolve('/')}>Search</a>
    </nav>
  {/snippet}
  {@render children()}
</Shell>
