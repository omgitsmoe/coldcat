export interface Schema {
  ref?: string;
  type?: string | string[];
  required?: string[];
  additionalProperties?: boolean;
  properties?: Record<string, Schema>;
  items?: Schema;
  const?: unknown;
  enum?: unknown[];
  pattern?: string;
  format?: string;
  minimum?: number;
  maximum?: number;
  minLength?: number;
  maxLength?: number;
  minItems?: number;
  maxItems?: number;
  minProperties?: number;
}

export interface Operation {
  method: string;
  path: string;
  status: number;
  query: Record<string, { schema: Schema; required: boolean }>;
  response: Schema;
  body?: Schema;
}
