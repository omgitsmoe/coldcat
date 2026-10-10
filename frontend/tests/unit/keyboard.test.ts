import { describe, expect, it } from 'vitest';
import { searchKeyAction } from '../../src/lib/state/keyboard';

const options = {
  workspace: true,
  desktop: true,
  typeToSearch: true,
  selection: false,
  dialog: false,
};
describe('keyboard ownership', () => {
  it('allows explicit focus elsewhere but printable typing only in the desktop workspace', () => {
    expect(searchKeyAction(new KeyboardEvent('keydown', { key: '/' }), options)).toBe('focus');
    expect(
      searchKeyAction(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }), {
        ...options,
        workspace: false,
      }),
    ).toBe('focus');
    expect(searchKeyAction(new KeyboardEvent('keydown', { key: '雪' }), options)).toBe('type');
    for (const change of [
      { workspace: false },
      { desktop: false },
      { typeToSearch: false },
      { selection: true },
      { dialog: true },
    ]) {
      expect(
        searchKeyAction(new KeyboardEvent('keydown', { key: 'a' }), { ...options, ...change }),
      ).toBe(null);
    }
  });
  it('ignores handled events, modifiers, IME/dead keys, editables and interactive controls', () => {
    for (const init of [
      { key: 'a', ctrlKey: true },
      { key: 'a', altKey: true },
      { key: 'a', isComposing: true },
      { key: 'Dead' },
      { key: 'Process' },
      { key: 'Enter' },
    ]) {
      expect(searchKeyAction(new KeyboardEvent('keydown', init), options)).toBe(null);
    }
    const handled = new KeyboardEvent('keydown', { key: 'a', cancelable: true });
    handled.preventDefault();
    expect(searchKeyAction(handled, options)).toBe(null);
    for (const tag of ['input', 'textarea', 'select', 'button', 'a']) {
      const target = document.createElement(tag);
      const event = new KeyboardEvent('keydown', { key: '/', bubbles: true });
      target.addEventListener('keydown', (e) => expect(searchKeyAction(e, options)).toBe(null));
      target.dispatchEvent(event);
    }
    const editable = document.createElement('div');
    editable.setAttribute('contenteditable', 'true');
    const child = editable.appendChild(document.createElement('span'));
    child.addEventListener('keydown', (e) => expect(searchKeyAction(e, options)).toBe(null));
    child.dispatchEvent(new KeyboardEvent('keydown', { key: 'a', bubbles: true }));
  });
});
