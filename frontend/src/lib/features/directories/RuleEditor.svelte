<script lang="ts">
  import { ruleInput, type Rules } from './rules';
  let {
    draft,
    changed,
    apply,
    scope = 'trees',
  }: {
    draft: Rules;
    changed: (rules: Rules) => void;
    apply: (rules: Rules) => void;
    scope?: 'trees' | 'source';
  } = $props();
  const error = $derived.by(() => {
    try {
      ruleInput(draft);
      return '';
    } catch (error) {
      return error instanceof Error ? error.message : 'Invalid rules';
    }
  });
  function update(kind: keyof Rules, index: number, value: string) {
    changed({ ...draft, [kind]: draft[kind].map((row, i) => (i === index ? value : row)) });
  }
</script>

<form
  class="rule-editor panel"
  onsubmit={(event) => {
    event.preventDefault();
    if (!error) apply(ruleInput(draft));
  }}
>
  <header class="actions">
    <h3>Draft allow / block rules</h3>
    <details class="syntax-help">
      <summary>Rule syntax and limits</summary>
      <p>
        One literal pattern per row; whitespace and backslash escaping are significant. Rules match
        relative file paths, case-sensitively, {scope === 'trees'
          ? 'on both trees'
          : 'in the source selection only; destination paths are ignored'}. Block wins over allow.
        No allow rules selects all files before blocking. The backend interprets doublestar globs.
      </p>
      <p>
        Examples: <code>**/*.jpg</code>, <code>{'{photos,video}/**'}</code>,
        <code>file\?.txt</code> (literal question mark). Maximum 100 rules combined, 1–1024 UTF-8 bytes
        per row and 16384 UTF-8 bytes combined; no NUL.
      </p>
    </details>
  </header>
  <p class="muted">
    No allow rules selects all files; block wins. Editing does not run a comparison.
  </p>
  <div class="rule-groups">
    {#each ['allow', 'block'] as kind (kind)}
      {@const key = kind as keyof Rules}
      {@const label = key === 'allow' ? 'Allow' : 'Block'}
      <fieldset>
        <legend>{label} rules</legend>
        {#each draft[key] as row, index (index)}
          <div class="rule-row">
            <label class="field"
              >{label} pattern {index + 1}
              <input
                class="literal"
                value={row}
                oninput={(event) => update(key, index, event.currentTarget.value)}
              />
            </label>
            <button
              type="button"
              class="button-quiet button-compact"
              aria-label={`Remove ${key} rule ${index + 1}`}
              onclick={() => changed({ ...draft, [key]: draft[key].filter((_, i) => i !== index) })}
              >Remove</button
            >
          </div>
        {/each}
        {#if !draft[key].length}<p class="muted">
            {label === 'Allow' ? 'All files allowed' : 'No files blocked'}
          </p>{/if}
        <button
          class="button-secondary button-compact"
          type="button"
          onclick={() => changed({ ...draft, [key]: [...draft[key], ''] })}
        >
          Add {key} rule
        </button>
      </fieldset>
    {/each}
  </div>
  {#if error}<p role="alert">{error}</p>{/if}
  <button class="button-primary" disabled={!!error}>Apply and compare</button>
</form>

<style>
  .rule-editor {
    display: grid;
    gap: var(--space-3);
  }
  h3,
  p {
    margin: 0;
  }
  header {
    justify-content: space-between;
    align-items: baseline;
  }
  .syntax-help {
    max-width: 44rem;
  }
  .syntax-help p {
    margin-top: var(--space-2);
  }
  .rule-groups {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--space-3);
  }
  fieldset {
    margin: 0;
    min-width: 0;
  }
  fieldset > p {
    margin-bottom: var(--space-2);
  }
  .rule-row {
    display: flex;
    gap: var(--space-2);
    align-items: end;
    margin-bottom: var(--space-2);
  }
  .field {
    flex: 1;
    min-width: 0;
  }
  input {
    width: 100%;
  }
  form > button {
    justify-self: start;
  }
  @media (max-width: 899px) {
    .rule-groups {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
