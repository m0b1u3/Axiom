import {createHash} from 'node:crypto';

type ObjectValue=Record<string,unknown>;
const object=(v:unknown):ObjectValue=>v!==null&&typeof v==='object'&&!Array.isArray(v)?v as ObjectValue:{};
const keys=(v:ObjectValue,expected:string[])=>Object.keys(v).sort().join('\0')===[...expected].sort().join('\0');
const integer=(v:unknown)=>Number.isSafeInteger(v)&&(v as number)>=0?v as number:null;
const described=(v:ObjectValue)=>typeof v.description==='string'&&v.description.trim().length>0;
export interface StandaloneValidationContractSummary {
 kind:'standalone_validation';schemaSha256:string;strict:boolean;
 reportRequirement:'nonempty_string_only'|'length_only'|'additional_constraints_unassessed';
 reportMinLength:number|null;reportMaxLength:number|null;reportPatternPresent:boolean;
 reportDescriptionPresent:boolean;dispositionDescriptionPresent:boolean;
}
/** Recognize only the native two-field standalone validate contract. Never log
 * arbitrary schemas, descriptions, regexes, enum strings or prompt contents.
 * This describes syntax; it does not identify a model's generation behavior. */
export function standaloneValidationContract(format:unknown):StandaloneValidationContractSummary|null {
 const outer=object(format);if(outer.type!=='json_schema')return null;
 const config=outer.schema!==undefined?outer:object(outer.json_schema),schema=object(config.schema),properties=object(schema.properties),disposition=object(properties.disposition),report=object(properties.report);
 if(schema.type!=='object'||schema.additionalProperties!==false||!keys(properties,['disposition','report'])||!Array.isArray(schema.required)||schema.required.length!==2||!schema.required.every(v=>typeof v==='string')||[...schema.required].sort().join('\0')!=='disposition\0report'||disposition.type!=='string'||!Array.isArray(disposition.enum)||disposition.enum.length!==4||![...disposition.enum].every(v=>typeof v==='string')||[...disposition.enum].sort().join('\0')!=='deferred\0not_applicable\0reportable\0suppressed'||report.type!=='string')return null;
 const min=integer(report.minLength),max=integer(report.maxLength);
 const validBounds=(!Object.hasOwn(report,'minLength')||min!==null)&&(!Object.hasOwn(report,'maxLength')||max!==null);
 const lengthOnly=validBounds&&Object.keys(report).every(k=>['type','minLength','maxLength','title','description'].includes(k))&&Object.keys(schema).every(k=>['type','properties','required','additionalProperties','$schema','title','description'].includes(k))&&Object.keys(disposition).every(k=>['type','enum','title','description'].includes(k));
 return {kind:'standalone_validation',schemaSha256:createHash('sha256').update(JSON.stringify(schema)).digest('hex'),strict:config.strict===true,reportRequirement:lengthOnly?(min===1&&max===null?'nonempty_string_only':'length_only'):'additional_constraints_unassessed',reportMinLength:min,reportMaxLength:max,reportPatternPresent:Object.hasOwn(report,'pattern'),reportDescriptionPresent:described(report),dispositionDescriptionPresent:described(disposition)};
}
export interface StructuredOutputControls {
 outputTokenLimitField:'max_output_tokens'|'max_completion_tokens'|'max_tokens'|null;
 outputTokenLimit:number|null;verbosity:'low'|'medium'|'high'|null;stopSpecified:boolean;
}
/** Record only known numeric/enum controls. Presence is not proof that an
 * upstream gateway honored a setting; missing controls remain unknown. */
export function structuredOutputControls(body:ObjectValue):StructuredOutputControls {
 const field=(['max_output_tokens','max_completion_tokens','max_tokens'] as const).find(k=>Object.hasOwn(body,k))??null;
 const verbosity=object(body.text).verbosity;
 return {outputTokenLimitField:field,outputTokenLimit:field===null?null:integer(body[field]),verbosity:verbosity==='low'||verbosity==='medium'||verbosity==='high'?verbosity:null,stopSpecified:Object.hasOwn(body,'stop')||Object.hasOwn(body,'stop_sequences')};
}
