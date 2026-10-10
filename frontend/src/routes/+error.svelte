<script lang="ts">
  import { page } from '$app/state';
</script>

<svelte:head><title>{page.status} · Coldcat</title></svelte:head>
<h1>{page.status === 404 ? 'Page not found' : 'Unable to open page'}</h1>
<p>This application route is unavailable.</p>
