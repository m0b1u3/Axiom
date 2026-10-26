import {createHash} from 'node:crypto';

type Json = Record<string, unknown>;
const object = (v: unknown): Json | undefined => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as Json : undefined;
const inputRoles = new Set(['system', 'developer', 'user']);

export class ResponsesInputError extends Error {
  constructor(readonly path: string, readonly code: 'responses_input_empty' | 'responses_input_invalid' | 'responses_input_reference' = 'responses_input_invalid') {
    super(`Responses 输入校验失败（${path}）；请求未发送，未自动重试。`);
  }
}

/** Public Responses messages carry the text itself. Codex's client-assigned
 * input IDs and telemetry stay in its local session, not in the provider wire.
 * Never rewrite server output IDs, item references, tool IDs or opaque items. */
export function prepareResponsesInput(raw: Buffer): Buffer {
  const body = object(JSON.parse(raw.toString('utf8')));
  if (!body) throw new ResponsesInputError('input');
  const context = Boolean(body.previous_response_id || body.conversation || (typeof body.instructions === 'string' && body.instructions.trim()));
  const input = body.input;
  if (input === undefined && !context) throw new ResponsesInputError('input', 'responses_input_empty');
  if (input !== undefined && typeof input !== 'string' && !Array.isArray(input)) throw new ResponsesInputError('input');
  if ((typeof input === 'string' && !input.trim() || Array.isArray(input) && input.length === 0) && !context) throw new ResponsesInputError('input', 'responses_input_empty');
  let changed = Object.hasOwn(body, 'client_metadata');
  const references = new Set(Array.isArray(input) ? input.flatMap(v => {
    const item = object(v);
    return item && typeof item.id === 'string' && (item.type === 'item_reference' || item.type == null && item.role === undefined && item.content === undefined) ? [item.id] : [];
  }) : []);
  const normalized = Array.isArray(input) ? input.map((value, index) => {
    const item = object(value);
    if (!item) throw new ResponsesInputError(`input[${index}]`);
    if (item.type !== 'message' && !(item.type === undefined && typeof item.role === 'string')) return value;
    if (!inputRoles.has(String(item.role))) return value;
    const content = item.content;
    if (typeof content !== 'string' && !Array.isArray(content)) throw new ResponsesInputError(`input[${index}].content`);
    if (typeof content === 'string' && !content.trim() || Array.isArray(content) && (content.length === 0 || content.every(v => {
      const part = object(v);
      return part?.type === 'input_text' && typeof part.text === 'string' && !part.text.trim();
    }))) throw new ResponsesInputError(`input[${index}].content`, 'responses_input_empty');
    if (Array.isArray(content)) for (const [partIndex, value] of content.entries()) {
      const part = object(value);
      if (!part || part.type === 'input_text' && typeof part.text !== 'string') throw new ResponsesInputError(`input[${index}].content[${partIndex}]`);
    }
    if (!Object.hasOwn(item, 'id') && !Object.hasOwn(item, 'internal_chat_message_metadata_passthrough')) return value;
    if (typeof item.id === 'string' && references.has(item.id)) throw new ResponsesInputError(`input[${index}].id`, 'responses_input_reference');
    const result = {...item};
    delete result.id;
    delete result.internal_chat_message_metadata_passthrough;
    changed = true;
    return result;
  }) : input;
  if (!changed) return raw;
  const result: Json = {...body, ...(input !== undefined ? {input: normalized} : {})};
  delete result.client_metadata;
  return Buffer.from(JSON.stringify(result));
}

/** Bounded facts only: never retain text, IDs, metadata, URLs or tool arguments. */
export function responsesInputSummary(value: unknown) {
  const input = object(value)?.input;
  const texts: string[] = [];
  const roles: Record<string, number> = {};
  let messages = 0, otherItems = 0;
  if (typeof input === 'string') texts.push(input);
  if (Array.isArray(input)) for (const value of input) {
    const item = object(value);
    if (!item || item.type !== 'message' && item.role === undefined) { otherItems++; continue; }
    messages++;
    const role = ['system', 'developer', 'user', 'assistant'].includes(String(item.role)) ? String(item.role) : 'other';
    roles[role] = (roles[role] ?? 0) + 1;
    if (typeof item.content === 'string') texts.push(item.content);
    if (Array.isArray(item.content)) for (const value of item.content) {
      const part = object(value);
      if (part?.type === 'input_text' && typeof part.text === 'string') texts.push(part.text);
    }
  }
  return {
    kind: typeof input === 'string' ? 'string' : Array.isArray(input) ? 'array' : input === undefined ? 'missing' : 'invalid',
    items: Array.isArray(input) ? input.length : typeof input === 'string' ? 1 : 0,
    messages, otherItems, roles, textParts: texts.length,
    nonemptyTextParts: texts.filter(v => v.trim()).length,
    textCharacters: texts.reduce((n, v) => n + Array.from(v).length, 0),
    textSha256: createHash('sha256').update(JSON.stringify(texts)).digest('hex'),
  };
}
