import type { Query } from '../../api/client';
import { contract, schemas } from '../../api/generated/contract';
import { assertSchema } from '../../api/decode';

type Lookup = Query<'GET /api/v1/contents/lookup'>;
export const algorithms = schemas.Algorithm.enum as Lookup['hash_type'][];

export function lookupInput(algorithm: string, digest: string): Lookup {
  assertSchema(algorithm, schemas.Algorithm, 'algorithm', 'request');
  try {
    assertSchema(
      digest,
      contract['GET /api/v1/contents/lookup'].query.hash!.schema,
      'digest',
      'request',
    );
  } catch {
    throw new Error('Digest must contain complete hexadecimal byte pairs, without spaces.');
  }
  return { hash_type: algorithm as Lookup['hash_type'], hash: digest, scope: 'history' };
}
