import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {validationReportBodyState} from '@axiom/contracts';
const cases = JSON.parse(await readFile(new URL('../../contracts/fixtures/validation-report-body.json', import.meta.url), 'utf8')) as Array<{name:string;report:string;state:string}>;
for (const c of cases) test('validation report structure: '+c.name, () => assert.equal(validationReportBodyState(c.report), c.state));
