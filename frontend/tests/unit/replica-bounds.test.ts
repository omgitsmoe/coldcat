import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';
import ReplicaBounds from '../../src/lib/components/ReplicaBounds.svelte';

afterEach(cleanup);

describe('replica bound controls', () => {
  it('Any shows only the matching mode; an explicit choice reveals the corresponding fields', async () => {
    render(ReplicaBounds, { mode: 'any', exact: '0', minimum: '1', maximum: '2' });
    const mode = screen.getByRole('combobox', { name: 'Other-copy matching' });
    expect(screen.queryByRole('textbox')).toBeNull();
    await fireEvent.change(mode, { target: { value: 'exact' } });
    expect(
      (screen.getByRole('textbox', { name: 'Exact other copies' }) as HTMLInputElement).value,
    ).toBe('0');
    expect(screen.queryByRole('textbox', { name: 'Minimum other copies' })).toBeNull();
    await fireEvent.change(mode, { target: { value: 'range' } });
    expect(screen.queryByRole('textbox', { name: 'Exact other copies' })).toBeNull();
    expect(
      (screen.getByRole('textbox', { name: 'Minimum other copies' }) as HTMLInputElement).value,
    ).toBe('1');
    expect(
      (screen.getByRole('textbox', { name: 'Maximum other copies' }) as HTMLInputElement).value,
    ).toBe('2');
  });

  it('retains edited values through mode changes rather than silently clearing them', async () => {
    render(ReplicaBounds, { mode: 'exact', exact: '0', minimum: '', maximum: '' });
    const mode = screen.getByRole('combobox', { name: 'Other-copy matching' });
    await fireEvent.input(screen.getByRole('textbox', { name: 'Exact other copies' }), {
      target: { value: '9007199254740993' },
    });
    await fireEvent.change(mode, { target: { value: 'any' } });
    await fireEvent.change(mode, { target: { value: 'exact' } });
    expect(
      (screen.getByRole('textbox', { name: 'Exact other copies' }) as HTMLInputElement).value,
    ).toBe('9007199254740993');
  });

  it('exposes all conflicting bookmarked values and invalid state without disabling correction', () => {
    render(ReplicaBounds, {
      mode: 'conflict',
      exact: '0',
      minimum: '1',
      maximum: '2',
      invalid: true,
    });
    expect(screen.getByRole('option', { name: 'Conflicting URL bounds' })).toBeTruthy();
    expect(screen.getAllByRole('textbox')).toHaveLength(3);
    for (const control of [screen.getByRole('combobox'), ...screen.getAllByRole('textbox')]) {
      expect(control.getAttribute('aria-invalid')).toBe('true');
      expect(control.hasAttribute('disabled')).toBe(false);
    }
  });
});
