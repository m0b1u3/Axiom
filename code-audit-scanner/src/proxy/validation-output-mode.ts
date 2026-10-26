import {standaloneValidationContract} from './validation-output-contract.js';

export type ValidationOutputMode = 'native_schema' | 'json_object';
export function validationOutputMode(value: string | undefined): ValidationOutputMode {
  if (value === undefined || value === 'native_schema') return 'native_schema';
  if (value === 'json_object') return value;
  throw new Error('AXIOM_VALIDATION_OUTPUT_MODE must be native_schema or json_object');
}
const object = (value: unknown): Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};

/** Only the recognized standalone validate contract may change. The SDK still
 * receives and strictly parses the original two-field result. Never retry,
 * repair output, alter tools or change the original report/evidence checks. */
export function validationJSONRequest(raw: Buffer, mode: ValidationOutputMode): Buffer {
  if (mode === 'native_schema') return raw;
  const body = JSON.parse(raw.toString('utf8')) as Record<string, unknown>;
  const text = object(body.text);
  const format = object(text.format);
  // Tool turns without structured output have no format to replace.
  if (format.type === undefined || format.type === 'text') return raw;
  const contract = standaloneValidationContract(format);
  if (!contract || !contract.strict || contract.reportRequirement !== 'nonempty_string_only') {
    throw new Error('JSON compatibility requires the recognized strict standalone validation schema');
  }
  const schema = format.schema;
  const instructions = 'Return exactly one JSON object matching this schema: ' + JSON.stringify(schema) +
    '\nThe report must contain the full Markdown assessment with separate heading and body lines (encode newlines as \"\\n\"). Include rationale, source evidence, counterevidence and uncertainty. Do not invent evidence or claim runtime reproduction. JSON mode does not relax local validation.';
  if (body.instructions !== undefined && typeof body.instructions !== 'string') throw new Error('JSON compatibility requires text instructions');
  body.instructions = [body.instructions, instructions].filter(Boolean).join('\n\n');
  body.text = {...text, format: {type: 'json_object'}};
  return Buffer.from(JSON.stringify(body));
}
