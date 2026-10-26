import {createHash} from 'node:crypto';

type ObjectValue = Record<string, unknown>;
type Identity = {namespace: string; name: string; kind: 'function' | 'custom'; alias: string};
const record = (v: unknown): ObjectValue | undefined => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as ObjectValue : undefined;
const callable = (type: unknown): Identity['kind'] | undefined => type === 'function' || type === 'custom' ? type : undefined;
const callKind = (type: unknown): Identity['kind'] | undefined => type === 'function_call' || type === 'function_call_output' ? 'function' : type === 'custom_tool_call' || type === 'custom_tool_call_output' ? 'custom' : undefined;
const key = (namespace: string, name: string, kind: string) => JSON.stringify([namespace, name, kind]);
export class NamespaceToolError extends Error {
  constructor() { super('工具分组兼容处理失败：工具身份、调用历史或返回格式不一致；请求未重放。'); }
}

/** One registry per SDK transport. Aliases are stable across turns and workers;
 * each response may resolve only the tools exposed by its own request. */
export class NamespaceTools {
  private readonly known = new Map<string, Identity>();

  prepare(raw: Buffer, compacting = false): {body: Buffer; restore: (value: unknown) => unknown} {
    const body = record(JSON.parse(raw.toString('utf8')));
    if (!body) throw new NamespaceToolError();
    const identities = new Map(this.known);
    const active = new Map<string, Identity>();
    const plain = new Set<string>();
    const exposed = new Set<string>();
    let changed = false;
    if (body.tools !== undefined && !Array.isArray(body.tools)) throw new NamespaceToolError();
    for (const value of body.tools as unknown[] ?? []) {
      const tool = record(value);
      if (tool?.type !== 'namespace' && typeof tool?.name === 'string') plain.add(tool.name);
    }
    const tools: unknown[] = [];
    for (const value of body.tools as unknown[] ?? []) {
      const tool = record(value);
      if (tool?.type !== 'namespace') { tools.push(value); continue; }
      if (typeof tool.name !== 'string' || !tool.name || !Array.isArray(tool.tools) || tool.tools.length === 0) throw new NamespaceToolError();
      for (const value of tool.tools) {
        const nested = record(value), kind = callable(nested?.type);
        if (!nested || !kind || typeof nested.name !== 'string' || !nested.name) throw new NamespaceToolError();
        const identityKey = key(tool.name, nested.name, kind);
        if (exposed.has(identityKey)) throw new NamespaceToolError();
        exposed.add(identityKey);
        const clean = (v: string) => v.replace(/[^A-Za-z0-9_-]/g, '_').slice(0, 15);
        const alias = `axns_${clean(tool.name)}_${clean(nested.name)}_${createHash('sha256').update(identityKey).digest('hex').slice(0, 24)}`;
        const identity: Identity = {namespace: tool.name, name: nested.name, kind, alias};
        if (plain.has(alias) || (identities.has(alias) && key(identities.get(alias)!.namespace, identities.get(alias)!.name, identities.get(alias)!.kind) !== identityKey)) throw new NamespaceToolError();
        identities.set(alias, identity); active.set(alias, identity);
        tools.push({...nested, name: alias, description: [nested.description, `Tool group ${tool.name}: ${typeof tool.description === 'string' ? tool.description : ''}`].filter(Boolean).join('\n')});
      }
      changed = true;
    }
    for (const name of plain) if (identities.has(name)) throw new NamespaceToolError();
    const flattenIdentity = (item: ObjectValue, kind: Identity['kind']): ObjectValue => {
      if (item.namespace === undefined || item.namespace === null) return item;
      if (item.name == null && String(item.type).endsWith('_output') && typeof item.call_id === 'string') {
        const result = {...item}; delete result.namespace; changed = true; return result;
      }
      if (typeof item.namespace !== 'string' || typeof item.name !== 'string') throw new NamespaceToolError();
      const identity = [...identities.values()].find(v => v.namespace === item.namespace && v.name === item.name && v.kind === kind);
      if (!identity) throw new NamespaceToolError();
      if (compacting && body.tools === undefined) active.set(identity.alias,identity);
      const result: ObjectValue = {...item, name: identity.alias}; delete result.namespace; changed = true; return result;
    };
    const input = Array.isArray(body.input) ? body.input.map(value => {
      const item = record(value), kind = callKind(item?.type);
      return item && kind ? flattenIdentity(item, kind) : value;
    }) : body.input;
    const choice = (value: unknown): unknown => {
      const item = record(value); if (!item) return value;
      const kind = callable(item.type);
      if (kind) {
        const result=flattenIdentity(item, kind);
        if (typeof result.name==='string' && identities.has(result.name) && !active.has(result.name)) throw new NamespaceToolError();
        return result;
      }
      if (item.type === 'allowed_tools' && Array.isArray(item.tools)) return {...item, tools: item.tools.map(choice)};
      if (item.type === 'namespace') throw new NamespaceToolError();
      return value;
    };
    const toolChoice = choice(body.tool_choice);
    const restoreItem = (value: unknown): unknown => {
      const item = record(value), kind = callKind(item?.type);
      if (!item || !kind || typeof item.name !== 'string') return value;
      const identity = active.get(item.name);
      if (!identity) {
        if (identities.has(item.name)) throw new NamespaceToolError();
        if (item.namespace != null && ![...active.values()].some(v=>v.namespace===item.namespace && v.name===item.name && v.kind===kind)) throw new NamespaceToolError();
        return value;
      }
      if (identity.kind !== kind || (item.namespace != null && item.namespace !== identity.namespace)) throw new NamespaceToolError();
      return {...item, name: identity.name, namespace: identity.namespace};
    };
    const restoreResponse = (value: unknown): unknown => {
      const obj = record(value);
      return obj && Array.isArray(obj.output) ? {...obj, output: obj.output.map(restoreItem)} : value;
    };
    const restore = (value: unknown): unknown => {
      const event = record(value); if (!event) return value;
      if (event.type === 'response.output_item.added' || event.type === 'response.output_item.done') return {...event, item: restoreItem(event.item)};
      if (['response.completed', 'response.failed', 'response.incomplete', 'response.created', 'response.in_progress'].includes(String(event.type))) return {...event, response: restoreResponse(event.response)};
      if (event.type === 'response.function_call_arguments.done' || event.type === 'response.custom_tool_call_input.done') {
        if (typeof event.name !== 'string') return value;
        const kind = event.type === 'response.function_call_arguments.done' ? 'function' : 'custom';
        const restored = restoreItem({...event, type: kind === 'function' ? 'function_call' : 'custom_tool_call'}) as ObjectValue;
        return {...restored, type: event.type};
      }
      return restoreResponse(value);
    };
    for (const [alias, identity] of identities) this.known.set(alias, identity);
    if (!changed) return {body: raw, restore};
    return {body: Buffer.from(JSON.stringify({...body, ...(body.tools !== undefined ? {tools} : {}), ...(body.input !== undefined ? {input} : {}), ...(body.tool_choice !== undefined ? {tool_choice: toolChoice} : {})})), restore};
  }
}
