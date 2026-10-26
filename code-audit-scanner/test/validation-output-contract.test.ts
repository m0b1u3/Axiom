import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
import {validationReportBodyState} from '@axiom/contracts';
import {readFile} from 'node:fs/promises';
import {pathToFileURL,fileURLToPath} from 'node:url';
import {standaloneValidationContract,structuredOutputControls} from '../src/proxy/validation-output-contract.js';
import {UpstreamTurnDiagnostic,type TurnDiagnostic} from '../src/proxy/turn-diagnostic.js';
const schema={type:'object',properties:{disposition:{type:'string',enum:['reportable','suppressed','not_applicable','deferred']},report:{type:'string',minLength:1}},required:['disposition','report'],additionalProperties:false};
const format={type:'json_schema',name:'fixture_validation',strict:true,schema};
const digest=(v:string)=>createHash('sha256').update(v).digest('hex');

test('native standalone contract requires a nonempty report but does not express report body semantics',()=>{
 const v=standaloneValidationContract(format)!;assert.equal(v.kind,'standalone_validation');assert.equal(v.reportRequirement,'nonempty_string_only');assert.equal(v.reportMinLength,1);assert.equal(v.reportMaxLength,null);assert.equal(v.reportDescriptionPresent,false);assert.equal(v.dispositionDescriptionPresent,false);assert.equal(v.schemaSha256,digest(JSON.stringify(schema)));assert.equal(v.strict,true);
});
test('Chat and Responses contract shapes have the same schema digest without a protocol switch',()=>{
 const chat={type:'json_schema',json_schema:{name:'fixture_validation',strict:true,schema}};assert.deepEqual(standaloneValidationContract(chat),standaloneValidationContract(format));assert.equal(chat.type,'json_schema');
});
test('length and description changes do not pretend to guarantee evidence or assessment correctness',()=>{
 const described=structuredClone(format);Object.assign(described.schema.properties.report,{description:'Complete Markdown assessment; this description is advisory.'});const summary=standaloneValidationContract(described)!;assert.equal(summary.reportRequirement,'nonempty_string_only');assert.equal(summary.reportDescriptionPresent,true);
 const length=structuredClone(format);length.schema.properties.report.minLength=512;assert.equal(standaloneValidationContract(length)!.reportRequirement,'length_only');
 const pattern=structuredClone(format);Object.assign(pattern.schema.properties.report,{pattern:'fixture-secret-regex'});const found=standaloneValidationContract(pattern)!;assert.equal(found.reportRequirement,'additional_constraints_unassessed');assert.equal(found.reportPatternPresent,true);assert.doesNotMatch(JSON.stringify(found),/fixture-secret|Markdown/);
});
test('unrecognized/malformed schemas fail closed and never log arbitrary names or force request mutation',()=>{
 for(const value of [null,[],{type:'json_object'},{...format,schema:{...schema,required:[{},'report']}},{...format,schema:{...schema,properties:{...schema.properties,'fixture-private-field':{type:'string'}}}},{...format,schema:{...schema,properties:{...schema.properties,disposition:{type:'string',enum:['reportable','suppressed','deferred','fixture-private-enum']}}}}])assert.equal(standaloneValidationContract(value),null);
 const invalid=structuredClone(format);Object.assign(invalid.schema.properties.report,{maxLength:'not-an-integer'});assert.equal(standaloneValidationContract(invalid)!.reportRequirement,'additional_constraints_unassessed');
});
test('diagnostics record only safe limit values and stop presence, never stop strings or invalid enum data',()=>{
 assert.deepEqual(structuredOutputControls({}),{outputTokenLimitField:null,outputTokenLimit:null,verbosity:null,stopSpecified:false});
 assert.deepEqual(structuredOutputControls({max_output_tokens:32,text:{verbosity:'low'},stop:['fixture-private-stop']}),{outputTokenLimitField:'max_output_tokens',outputTokenLimit:32,verbosity:'low',stopSpecified:true});
 for(const field of ['max_tokens','max_completion_tokens'])assert.equal(structuredOutputControls({[field]:64}).outputTokenLimit,64);
 const invalid=structuredOutputControls({max_output_tokens:'fixture-private-limit',text:{verbosity:'fixture-private-verbosity'},stop_sequences:'fixture-private-stop'});assert.equal(invalid.outputTokenLimit,null);assert.equal(invalid.verbosity,null);assert.doesNotMatch(JSON.stringify(invalid),/fixture-private/);
});
test('recorded contract metadata is private, body-free and does not modify native request bytes',()=>{
 const described=structuredClone(format);Object.assign(described.schema.properties.report,{description:'fixture-private-description'});const raw=JSON.stringify({model:'fixture-private-model',input:'fixture-private-prompt',authorization:'Bearer fixture-private-key',text:{format:described},stop:['fixture-private-stop']});const before=Buffer.from(raw),receipts:TurnDiagnostic[]=[];
 const d=new UpstreamTurnDiagnostic(before,'responses',r=>receipts.push(r));d.finish(false);assert.equal(before.toString(),raw);assert.equal(receipts[0]!.request.sha256,digest(raw));assert.equal(receipts[0]!.request.validationContract?.reportDescriptionPresent,true);assert.equal(receipts[0]!.request.controls.stopSpecified,true);assert.doesNotMatch(JSON.stringify(receipts),/fixture-private|Bearer/);
});
test('a post-completion client close remains distinct from a stream without a completed answer',()=>{
 const text='{"disposition":"reportable","report":"## Fixture title"}',item={type:'message',content:[{type:'output_text',text}]},receipts:TurnDiagnostic[]=[];
 const d=new UpstreamTurnDiagnostic(Buffer.from('{}'),'responses',r=>receipts.push(r));d.headers(200,'text/event-stream');d.chunk(Buffer.from('data: '+JSON.stringify({type:'response.completed',response:{status:'completed',output:[item]}})+'\n\n'));d.finish(true);assert.equal(receipts[0]!.cancelled,true);assert.equal(receipts[0]!.response.status,'completed');assert.equal(receipts[0]!.response.terminalText.sha256,digest(text));
 const partial=new UpstreamTurnDiagnostic(Buffer.from('{}'),'responses',r=>receipts.push(r));partial.headers(200,'text/event-stream');partial.chunk(Buffer.from('data: '+JSON.stringify({type:'response.output_text.delta',delta:text})+'\n\n'));partial.finish(true);assert.equal(receipts[1]!.response.status,null);assert.equal(receipts[1]!.response.terminalText.bytes,0);assert.equal(receipts[1]!.cancelled,true);
});
test('the installed SDK standalone prompt requires assessment content and read-only evidence, not a title-only answer',async()=>{
 const root=fileURLToPath(new URL('../node_modules/@openai/codex-security/',import.meta.url));const {standaloneValidationPrompt}=await import(pathToFileURL(root+'dist/standalone-validation-prompt.js').href);const text=await standaloneValidationPrompt(root+'_bundled_plugin');
 for(const requirement of [/Include its identifier and title, disposition, source locations, rationale, static confidence, supporting evidence, counterevidence, material proof gaps, and context used/,/return the assessment directly/,/Do not claim runtime reproduction or test execution/,/Do not execute application code/])assert.match(text,requirement);
 const api=await readFile(root+'dist/api.js','utf8');assert.match(api,/Return the disposition and the full Markdown assessment as report, including root cause and exploitability/);assert.match(api,/report: z\.string\(\)\.trim\(\)\.min\(1\)/);
});

test('the captured SDK format matches the exact retained review-11 format digest',async()=>{
 const fixture=JSON.parse(await readFile(new URL('../../contracts/fixtures/sdk050-standalone-validation-format.json',import.meta.url),'utf8'));assert.equal(fixture.sdkVersion,'0.5.0');assert.equal(digest(JSON.stringify(fixture.format)),fixture.formatSha256);assert.equal(fixture.formatSha256,'146d5f8d876052c923acf7d97fdcfeb2cc5aee884e23736a42310a4ebe1b5d23');assert.equal(standaloneValidationContract(fixture.format)!.reportRequirement,'nonempty_string_only');assert.equal(fixture.format.schema.properties.report.maxLength,undefined);
});
test('strict native JSON syntax admits a title; descriptions or a longer title do not establish assessment evidence',async()=>{
 const fixture=JSON.parse(await readFile(new URL('../../contracts/fixtures/sdk050-standalone-validation-format.json',import.meta.url),'utf8'));const require=createRequire(new URL('../../contracts/package.json',import.meta.url)),Ajv=require('ajv'),schema=fixture.format.schema,validate=new Ajv().compile(schema);
 const heading='## Fixtures / Mock assessment';for(const disposition of ['reportable','suppressed','not_applicable','deferred'])assert.equal(validate({disposition,report:heading}),true);
 assert.equal(validationReportBodyState(heading),'title_only');assert.equal(validate({disposition:'verified',report:heading}),false);assert.equal(validate({disposition:'reportable',report:{title:heading}}),false);assert.equal(validate({disposition:'reportable',report:heading,extra:true}),false);
 const annotated=structuredClone(schema);annotated.properties.report.description='Full Markdown assessment with rationale and evidence';assert.equal(new Ajv().compile(annotated)({disposition:'reportable',report:heading}),true);
 const length=structuredClone(schema);length.properties.report.minLength=512;const padded='## '+('Fixture title '.repeat(50));assert.equal(new Ajv().compile(length)({disposition:'reportable',report:padded}),true);assert.equal(validationReportBodyState(padded),'title_only');
 const brief='## Assessment\nOnly a test fixture.';assert.equal(validate({disposition:'not_applicable',report:brief}),true);assert.equal(validationReportBodyState(brief),'body_present');
});
