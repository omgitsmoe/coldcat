export type ErrorKind = 'http' | 'transport' | 'contract' | 'cancelled' | 'request';

export class ApiError extends Error {
  readonly kind: ErrorKind;
  readonly status?: number;
  readonly code?: string;
  readonly uncertainWrite: boolean;

  constructor(
    kind: ErrorKind,
    message: string,
    details: { status?: number; code?: string; uncertainWrite?: boolean; cause?: unknown } = {},
  ) {
    super(message, { cause: details.cause });
    this.name = 'ApiError';
    this.kind = kind;
    this.status = details.status;
    this.code = details.code;
    this.uncertainWrite = details.uncertainWrite === true;
  }
}
