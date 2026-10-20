// Fixtures / Mock only, invoked within the disposable Server pipeline.
import assert from 'node:assert/strict';

const safe='9007199254740991';
export async function configureNativeDeep(page,choose,shot,checks){
 await choose('扫描模式','深度扫描');
 await page.getByText('深度扫描停止条件（可选）',{exact:true}).click();
 await page.getByText('高级 SDK 参数',{exact:true}).click();
 const workers=page.locator('#deep-workers'),subagents=page.locator('#sdk-subagents');
 let creates=0;const count=r=>{if(r.method()==='POST'&&new URL(r.url()).pathname==='/api/v1/scans')creates++};page.on('request',count);
 for(const [field,value,pattern] of [[workers,'0',/Worker 数量须为正安全整数/],[subagents,'-1',/子代理数量须为非负安全整数/]]){
  await workers.fill('129');await subagents.fill('65');await field.fill(value);
  await page.getByRole('button',{name:'创建扫描',exact:true}).click();await page.getByText(pattern).first().waitFor();
  assert.equal(creates,0,'invalid SDK concurrency input reached Server');
 }
 for(const selector of ['#deep-workers','#deep-max-runs','#deep-stop-new','#sdk-subagents']){
  const input=page.locator(selector);assert.equal(await input.getAttribute('max'),safe);
  await input.fill('9007199254740992');assert.equal(await input.evaluate(el=>el.validity.rangeOverflow),true);
  await input.fill(safe);assert.equal(await input.evaluate(el=>el.validity.valid),true);
 }
 await page.locator('[data-sonner-toast]').last().waitFor({state:'hidden'});
 await shot('native-deep-desktop',page.locator('.deep-options'));
 await page.setViewportSize({width:390,height:844});await shot('native-deep-mobile',page.locator('.deep-options'));
 await page.setViewportSize({width:1440,height:1000});
 await workers.fill('129');await subagents.fill('65');await page.locator('#deep-max-runs').fill('1001');await page.locator('#deep-stop-new').fill('1002');
 await page.locator('#scan-timeout').fill('47');await page.locator('#scan-total-budget').fill('12');await page.locator('#scan-include').fill('api/a.ts');
 page.off('request',count);checks.push('native-deep-and-subagent-boundaries-desktop-mobile','invalid-native-inputs-create-no-task','operator-budget-timeout-and-scope-retained');
}

export async function inspectNativeCompatibility(page,id,shot,checks){
 const pattern='**/api/v1/executions/overview*';
 const handler=async route=>{
  const response=await route.fetch(),body=await response.json(),data=body.data;
  const task=data.tasks.find(t=>t.taskId===id),agent=data.agents[0];assert.ok(task&&agent);
  agent.online=true;agent.runtime.scannerDescriptionVersion=11;
  task.reason='sdk_concurrency_incompatible';task.compatibility.reason=task.reason;task.compatibility.required.sdkConcurrency=true;
  task.compatibility.candidates=[{agentId:agent.agentId,online:true,ready:false,missing:['sdk_concurrency']}];
  await route.fulfill({response,json:body});
 };
 await page.route(pattern,handler);await page.reload();
 const panel=page.getByRole('region',{name:'任务执行兼容性',exact:true});
 await panel.getByText('请同步更新 Agent 与 Scanner，以支持本次 SDK 原生并发参数；配置保持冻结，不会自动调低并发数。',{exact:true}).waitFor();
 await panel.getByRole('cell',{name:'SDK 原生并发参数',exact:true}).waitFor();
 await shot('native-compatibility-desktop',panel);await page.setViewportSize({width:390,height:844});await shot('native-compatibility-mobile',panel);await page.setViewportSize({width:1440,height:1000});
 await page.unroute(pattern,handler);checks.push('older-runtime-has-explicit-native-concurrency-upgrade-hint-without-clamping');
}

export async function configureNativeComponents(page,form,shot,checks){
 const workers=form.getByLabel('并发组件数',{exact:true});assert.equal(await workers.inputValue(),'4','default differs from SDK 0.1.25');
 assert.equal(await workers.getAttribute('max'),safe);
 for(const value of ['0','1.5','9007199254740992']){await workers.fill(value);assert.equal(await workers.evaluate(el=>el.validity.valid),false)}
 await workers.fill(safe);assert.equal(await workers.evaluate(el=>el.validity.valid),true);
 await shot('native-components-desktop',form.locator('.component-fields'));
 await page.setViewportSize({width:390,height:844});await shot('native-components-mobile',form.locator('.component-fields'));await page.setViewportSize({width:1440,height:1000});
 await workers.fill('9');checks.push('component-native-default-four-and-worker-nine','native-component-boundaries-desktop-mobile');
}
