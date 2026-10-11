import { parseContents, type ContentsRoute } from '../../state/routes';

export function parseMembershipDraft(params: URLSearchParams): ContentsRoute {
  const values = params.getAll('disk_id');
  if (values.length !== 1 || /^[1-9][0-9]*$/.test(values[0]!)) return parseContents(params);
  const validated = new URLSearchParams(params);
  validated.set('disk_id', '1');
  return { ...parseContents(validated), disk_id: values[0]! };
}
