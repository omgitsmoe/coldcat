import { validateGlobRules } from '../../api/query';

export type Rules = { allow: string[]; block: string[] };

export function ruleInput(draft: Rules): Rules {
  validateGlobRules(draft);
  return { allow: [...draft.allow], block: [...draft.block] };
}
