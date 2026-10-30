const object=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const maps=['properties','patternProperties','$defs','definitions','dependentSchemas'];
const singles=['items','additionalItems','additionalProperties','unevaluatedProperties','unevaluatedItems','propertyNames','contains','not','if','then','else','contentSchema'];
const lists=['anyOf','oneOf','allOf','prefixItems'];
const references=new Set(['$ref','$dynamicRef','$recursiveRef','$id','$anchor','$dynamicAnchor','$recursiveAnchor']);
export class SchemaUnionCompatibilityError extends Error{readonly code='schema_union_incompatible';}

/** OR is associative only for a branch containing nothing but another OR.
 * Preserve every assertion, reference, discriminator, annotation and strict
 * control. Never walk const/enum/example data as if it were a schema. */
export function flattenSchemaAnyOf(schema:unknown):{schema:unknown;changed:boolean}{
 // Moving an indexed branch could invalidate a JSON Pointer into it. The
 // native matching contract has no references; leave referenced schemas exact.
 function referenced(v:unknown,depth=0):boolean{
  if(depth>128)throw new SchemaUnionCompatibilityError('Structured schema nesting exceeds the compatibility limit');
  if(Array.isArray(v))return v.some(x=>referenced(x,depth+1));
  return object(v)&&Object.entries(v).some(([key,value])=>references.has(key)||referenced(value,depth+1));
 }
 if(referenced(schema))return {schema,changed:false};
 let changed=false;
 function visit(value:unknown,depth=0):unknown{
  if(!object(value))return value;
  if(depth>128)throw new SchemaUnionCompatibilityError('Structured schema nesting exceeds the compatibility limit');
  const out={...value};
  for(const key of maps)if(object(out[key]))out[key]=Object.fromEntries(Object.entries(out[key]).map(([name,v])=>[name,visit(v,depth+1)]));
  for(const key of singles)if(key in out)out[key]=Array.isArray(out[key])?(out[key] as unknown[]).map(v=>visit(v,depth+1)):visit(out[key],depth+1);
  for(const key of lists)if(Array.isArray(out[key]))out[key]=(out[key] as unknown[]).map(v=>visit(v,depth+1));
  if(Array.isArray(out.anyOf))out.anyOf=out.anyOf.flatMap(branch=>{
   if(object(branch)&&Object.keys(branch).length===1&&Array.isArray(branch.anyOf)&&branch.anyOf.length){changed=true;return branch.anyOf;}
   return [branch];
  });
  return out;
 }
 const result=visit(schema);return {schema:changed?result:schema,changed};
}

/** DeepSeek rejects Zod's bare nested anyOf in Responses output schemas.
 * The SDK still validates its original schema; no response is rewritten. */
export function flattenedResponseSchema(raw:Buffer):Buffer{
 const body=JSON.parse(raw.toString('utf8'));
 if(!object(body)||!object(body.text)||!object(body.text.format)||body.text.format.type!=='json_schema')return raw;
 const result=flattenSchemaAnyOf(body.text.format.schema);if(!result.changed)return raw;
 return Buffer.from(JSON.stringify({...body,text:{...body.text,format:{...body.text.format,schema:result.schema}}}));
}
