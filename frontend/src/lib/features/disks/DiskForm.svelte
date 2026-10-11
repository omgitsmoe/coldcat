<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { buildDiskPatch } from '../../api/disks';
  import { capacityBytes, CAPACITY_UNITS, type CapacityUnit } from '../../format/capacity';
  import { useConnection } from '../../state/context';
  import DiskMetadata from './DiskMetadata.svelte';

  type Disk = Result<'GET /api/v1/disks/{id}'>;
  let {
    initial,
    refreshed,
    closed,
  }: {
    initial?: Disk;
    refreshed: (disk: Disk) => void;
    closed: () => void;
  } = $props();
  const connection = useConnection();
  let before = $state<Disk>();
  let label = $state('');
  let capacity = $state('');
  let capacityUnit = $state<CapacityUnit>('B');
  let capacityTouched = $state(false);
  let labelTouched = $state(false);
  let discarding = $state(false);
  let heading: globalThis.HTMLHeadingElement;
  let checking = $state(false);
  const formId = $props.id();
  const capacityId = `${formId}-capacity`;
  let serial = $state('');
  let notes = $state('');
  let busy = $state(false);
  let blocked = $state(false);
  let error = $state('');
  let message = $state('');
  let verified = $state<Disk>();
  let created = $state(false);
  let cursor = $state<string | null | undefined>();
  let checkedRevision: string | undefined;
  let submittedLabel = '';
  let initialized = false;
  let ownInvalidation = false;
  let disposed = false;
  const controller = new globalThis.AbortController();

  $effect(() => {
    if (initialized) return;
    initialized = true;
    before = initial;
    if (initial) {
      label = initial.label;
      capacity = initial.capacity;
      serial = initial.serial ?? '';
      notes = initial.notes ?? '';
    }
  });
  const conversion = $derived.by(() => {
    try {
      return { bytes: capacityBytes(capacity, capacityUnit), error: '' };
    } catch (cause) {
      return { bytes: undefined, error: (cause as Error).message };
    }
  });
  const capacityError = $derived(capacityTouched ? conversion.error : '');
  const labelError = $derived(labelTouched && label === '' ? 'Enter a disk label.' : '');
  const dirty = $derived(
    before
      ? label !== before.label ||
          conversion.bytes !== before.capacity ||
          serial !== (before.serial ?? '') ||
          notes !== (before.notes ?? '')
      : label !== '' || capacity !== '' || capacityUnit !== 'B' || serial !== '' || notes !== '',
  );
  onMount(() => {
    heading.focus();
    heading.scrollIntoView({ block: 'nearest' });
  });

  function close() {
    if (busy || blocked) return;
    if (dirty) discarding = true;
    else closed();
  }
  const unbind = connection.onInvalidate(() => {
    if (ownInvalidation) return;
    verified = undefined;
    cursor = undefined;
    checkedRevision = undefined;
    if (before || blocked || busy) {
      blocked = true;
      error =
        'Catalog context changed. Check whether saved before submitting; your inputs are retained.';
      discarding = false;
    }
  });
  onDestroy(() => {
    disposed = true;
    controller.abort();
    unbind();
  });

  function invalidate() {
    ownInvalidation = true;
    connection.metadataChanged();
    ownInvalidation = false;
  }

  async function loadDisk(id: string) {
    const epoch = connection.epoch;
    const disk = await connection.client.disk(id, controller.signal);
    if (disposed) return;
    if (epoch !== connection.epoch)
      throw new Error('Context changed during reload; check whether saved again.');
    if (before) {
      if (label === before.label) label = disk.label;
      if (serial === (before.serial ?? '')) serial = disk.serial ?? '';
      if (notes === (before.notes ?? '')) notes = disk.notes ?? '';
      let capacityChanged: boolean;
      try {
        capacityChanged = capacityBytes(capacity, capacityUnit) !== before.capacity;
      } catch {
        capacityChanged = true;
      }
      if (!capacityChanged && disk.capacity !== before.capacity) {
        capacity = disk.capacity;
        capacityUnit = 'B';
      }
    }
    before = disk;
    verified = disk;
    invalidate();
    refreshed(disk);
    blocked = false;
    return disk;
  }

  function failure(cause: unknown, writing = false) {
    if (disposed) return;
    connection.report(cause);
    if (writing && cause instanceof ApiError && cause.uncertainWrite) {
      blocked = true;
      error =
        'The write may have succeeded. Check whether saved before resubmitting; no automatic retry.';
      invalidate();
    } else if (cause instanceof ApiError && cause.status === 409) {
      error = `Conflict: ${cause.message}. Your inputs are retained.`;
    } else error = cause instanceof Error ? cause.message : 'Disk request failed';
  }

  async function submit(event: globalThis.SubmitEvent) {
    event.preventDefault();
    if (busy || blocked || created) return;
    error = '';
    message = '';
    capacityTouched = true;
    labelTouched = true;
    discarding = false;
    if (conversion.error || labelError) return;
    let draft;
    try {
      if (label === '') throw new Error('Label must not be empty');
      draft = {
        label,
        capacity: capacityBytes(capacity, capacityUnit),
        serial: serial === '' ? null : serial,
        notes: notes === '' ? null : notes,
      };
    } catch (cause) {
      failure(cause);
      return;
    }
    const patch = before ? buildDiskPatch(before, draft) : undefined;
    if (patch && Object.keys(patch).length === 0) {
      message = 'No changes to save.';
      return;
    }
    busy = true;
    submittedLabel = label;
    const epoch = connection.epoch;
    let written = false;
    try {
      const disk = before
        ? await connection.client.updateDisk(before.id, patch!, controller.signal)
        : await connection.client.createDisk(draft, controller.signal);
      if (disposed) return;
      written = true;
      blocked = true;
      if (epoch !== connection.epoch) {
        throw new Error(
          'Catalog context changed during the write. Check whether saved before resubmitting.',
        );
      }
      const creating = !before;
      before = disk;
      invalidate();
      await loadDisk(disk.id);
      if (disposed) return;
      created = creating;
      message = creating
        ? 'Disk created and metadata refreshed.'
        : 'Disk updated and metadata refreshed.';
    } catch (cause) {
      failure(cause, !written);
      if (written)
        error = `Write succeeded but metadata is unverified. Check whether saved before resubmitting. ${error}`;
    } finally {
      if (!disposed) {
        busy = false;
        checking = false;
      }
    }
  }

  async function reconcile(next = false) {
    if (busy) return;
    if (!next) {
      cursor = undefined;
      checkedRevision = undefined;
    }
    busy = true;
    checking = true;
    error = '';
    message = '';
    try {
      if (before) {
        await loadDisk(before.id);
        if (!disposed) {
          created = !initial;
          message = 'Review refreshed metadata against your retained inputs before saving.';
        }
      } else {
        const epoch = connection.epoch;
        const page = await connection.client.disks(
          { limit: 50, ...(next && cursor ? { cursor } : {}) },
          controller.signal,
        );
        if (disposed) return;
        if (epoch !== connection.epoch)
          throw new Error('Context changed; restart the saved-change check.');
        if (next && checkedRevision !== page.revision) {
          cursor = undefined;
          throw new Error('Inventory changed; restart the saved-change check.');
        }
        checkedRevision = page.revision;
        const match = page.items.find((disk) => disk.label === submittedLabel);
        if (match) {
          cursor = undefined;
          await loadDisk(match.id);
          if (disposed) return;
          created = true;
          message = 'Existing disk found. Review its metadata; no creation was retried.';
          cursor = undefined;
        } else {
          cursor = page.next_cursor;
          message =
            cursor === null
              ? 'No matching label found. A disk might have been renamed; review the disk list before allowing another creation.'
              : 'No matching label on this page. Check the next disk page before resubmitting.';
          invalidate();
        }
      }
    } catch (cause) {
      cursor = undefined;
      checkedRevision = undefined;
      failure(cause);
    } finally {
      if (!disposed) {
        busy = false;
        checking = false;
      }
    }
  }
</script>

<section class="panel form-body" aria-label={initial ? 'Edit disk' : 'Create disk'}>
  <h2 bind:this={heading} tabindex="-1">{initial ? 'Edit disk' : 'Create disk'}</h2>
  <p class="muted">Label and capacity are required. Serial and notes are optional.</p>
  <form onsubmit={submit} novalidate aria-busy={busy}>
    <fieldset disabled={busy || created}>
      <label for={`${formId}-label`}>Label</label>
      <input
        id={`${formId}-label`}
        type="text"
        bind:value={label}
        oninput={() => (labelTouched = true)}
        aria-invalid={labelError ? 'true' : undefined}
        aria-describedby={labelError ? `${formId}-label-error` : undefined}
        required
      />
      {#if labelError}<p id={`${formId}-label-error`} class="status-error" role="alert">
          {labelError}
        </p>{/if}
      <div class="capacity-controls">
        <label for={capacityId}
          >Capacity amount
          <input
            id={capacityId}
            type="text"
            inputmode="decimal"
            bind:value={capacity}
            oninput={() => (capacityTouched = true)}
            aria-invalid={capacityError ? 'true' : undefined}
            aria-describedby={`${capacityId}-help ${capacityId}-preview${capacityError ? ` ${capacityId}-error` : ''}`}
            required
          />
        </label>
        <label
          >Capacity unit
          <select
            bind:value={capacityUnit}
            onchange={() => (capacityTouched = true)}
            aria-describedby={`${capacityId}-help`}
          >
            {#each CAPACITY_UNITS as unit (unit)}<option value={unit}>{unit}</option>{/each}
          </select>
        </label>
      </div>
      <p id={`${capacityId}-help`} class="muted">
        Changing the unit reinterprets the amount, not converts it. KB–TB use powers of 1000;
        KiB–TiB use powers of 1024. Declared capacity is not measured usage.
      </p>
      <p id={`${capacityId}-preview`} aria-live="polite">
        Exact bytes: <span class="literal">{conversion.bytes ?? '—'}</span>
      </p>
      {#if capacityError}
        <p id={`${capacityId}-error`} class="status-error" role="alert">{capacityError}</p>
      {/if}
      <label>Serial <input type="text" bind:value={serial} /></label>
      <label>Notes <textarea bind:value={notes}></textarea></label>
      <p class="muted">Empty serial or notes clears that field. Other text is preserved exactly.</p>
    </fieldset>
    <div class="actions">
      <button class="button-primary" type="submit" disabled={busy || blocked || created}>
        <span class="button-labels">
          <span aria-hidden={busy && !checking}>{initial ? 'Save changes' : 'Create disk'}</span>
          <span aria-hidden={!busy || checking}>Saving…</span>
        </span>
      </button>
      <button class="button-quiet" type="button" disabled={busy || blocked} onclick={close}>
        {created || (verified && !dirty) ? 'Close' : 'Cancel'}
      </button>
    </div>
  </form>
  {#if discarding}
    <div class="panel" aria-label="Discard draft">
      <p>
        Discard your unsaved changes? This only clears this draft; it does not undo saved changes.
      </p>
      <div class="actions">
        <button onclick={() => (discarding = false)}>Keep editing</button>
        <button onclick={closed}>Discard changes</button>
      </div>
    </div>
  {/if}
  {#if busy}<p role="status">{checking ? 'Checking whether saved…' : 'Saving…'}</p>{/if}
  {#if message}<p class="status-success" role="status">{message}</p>{/if}
  {#if blocked}
    <section class="panel" aria-label="Check whether saved">
      <h3>Check whether saved</h3>
      {#if error}<p class="status-warning" role="alert">{error}</p>{/if}
      <p>
        Your draft is retained. Checking reads the catalog, never retries the write. Cancel cannot
        undo a sent write; resolve this check before closing.
      </p>
      <button class="button-primary" disabled={busy} onclick={() => void reconcile()}>
        Check whether saved
      </button>
      {#if !before && cursor}<button disabled={busy} onclick={() => void reconcile(true)}
          >Check next disk page</button
        >{/if}
      {#if !before && cursor === null}
        <button
          disabled={busy}
          onclick={() => {
            blocked = false;
            cursor = undefined;
            error = '';
            message = 'Resubmission allowed by your explicit review. No write has been retried.';
          }}>I reviewed the disks; allow another creation</button
        >
      {/if}
    </section>
  {:else if error}
    <p class="status-error" role="alert">{error}</p>
  {/if}
  {#if verified}
    <details open={blocked || created}>
      <summary>Refreshed metadata — compare with your inputs</summary>
      <DiskMetadata item={verified} />
    </details>
    {#if created}<a href={resolve('/disks/[id]', { id: verified.id })}>Open verified disk</a>{/if}
  {/if}
</section>

<style>
  fieldset {
    display: grid;
    gap: 0.5rem;
    border: 0;
    padding: 0;
  }
  label {
    display: grid;
    gap: 0.2rem;
  }
  input,
  textarea {
    max-width: 40rem;
  }
  button {
    justify-self: start;
  }
  .capacity-controls {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: var(--space-2);
    max-width: 40rem;
  }
</style>
