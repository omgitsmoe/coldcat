<script lang="ts">
  import { page } from '$app/state';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { tick } from 'svelte';
  import { searchKeyAction } from '../../state/keyboard';

  function keydown(event: globalThis.KeyboardEvent) {
    if (page.url.pathname === '/') return;
    const action = searchKeyAction(event, {
      workspace: false,
      desktop: false,
      typeToSearch: false,
      selection: !!globalThis.window.getSelection()?.toString(),
      dialog: !!globalThis.document.querySelector(
        'dialog[open], [aria-modal="true"], [role="dialog"]:not([hidden])',
      ),
    });
    if (action !== 'focus') return;
    event.preventDefault();
    void goto(resolve('/'))
      .then(tick)
      .then(() =>
        globalThis.document.querySelector<globalThis.HTMLInputElement>('#query')?.focus(),
      );
  }
</script>

<svelte:window onkeydown={keydown} />
