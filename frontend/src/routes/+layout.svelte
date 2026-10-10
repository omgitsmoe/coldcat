<script lang="ts">
  import '../app.css';
  import { resolve } from '$app/paths';
  import Shell from '../lib/components/Shell.svelte';
  import GlobalShortcut from '../lib/features/search/GlobalShortcut.svelte';
  let { children } = $props();
</script>

<Shell>
  <GlobalShortcut />
  {#snippet brand()}<a class="brand" href={resolve('/')}>Coldcat</a>{/snippet}
  {#snippet navigation()}
    <nav aria-label="Main navigation">
      <a href={resolve('/')}>Search</a>
      <a href={resolve('/contents')}>Hash lookup</a>
      <a href={resolve('/disks')}>Disks</a>
    </nav>
  {/snippet}
  {@render children()}
</Shell>
