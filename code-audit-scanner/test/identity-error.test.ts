import {test} from 'node:test';import assert from 'node:assert/strict';
import {identityErrorCode} from '../src/runtime/identity-error.js';
test('identity diagnostics classify native failures without returning native secrets',()=>{
 for(const [message,expected] of [
  ['request failed: 403 Forbidden secret=fixture-private','sdk_auth_forbidden'],
  ['unsupported_country_region','sdk_auth_region_rejected'],
  ['HTTP 429 Too Many Requests','sdk_auth_rate_limited'],
  ['certificate verify failed','sdk_auth_tls_failed'],
  ['operation timed out','sdk_auth_timeout'],
  ['getaddrinfo ENOTFOUND auth.openai.com','sdk_auth_dns_failed'],
  ['error sending request for url (https://auth.openai.com/example?private=fixture)','sdk_auth_connection_failed'],
  ['spawn codex ENOENT','sdk_auth_runtime_missing'],
  ['invalid SDKDeviceInstructions','sdk_auth_instructions_invalid'],
  ['fixture-secret@example.test private token','sdk_auth_failed'],
 ])assert.equal(identityErrorCode(new Error(message)),expected);
 assert.equal(identityErrorCode(new Error('wrapper',{cause:new Error('ECONNREFUSED')})),'sdk_auth_connection_failed');
 const cyclic=new Error('fixture-private');cyclic.cause=cyclic;assert.equal(identityErrorCode(cyclic),'sdk_auth_failed');
});
