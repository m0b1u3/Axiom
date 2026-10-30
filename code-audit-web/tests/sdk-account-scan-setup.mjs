// Used only by the guarded, disposable identity pipeline browser fixture.
import assert from 'node:assert/strict';

export async function acceptAuthorizedScanSetup({page,base,snapshotId,checks,out}) {
 const defaultModel=await page.evaluate(async()=>{
  const headers={'Content-Type':'application/json',Authorization:'Bearer '+localStorage.getItem('axiom_token')};
  const response=await fetch('/api/v1/model-configs',{method:'POST',headers,body:JSON.stringify({name:'Fixtures / Mock DeepSeek default',provider:'deepseek',model:'fixture-deepseek',baseUrl:'https://fixture-provider.invalid/v1',apiKey:'fixture-never-real',wireApi:'responses'})});
  if(!response.ok)throw new Error('Default model fixture creation failed');
  const model=(await response.json()).data;
  const enabled=await fetch('/api/v1/model-configs/'+model.id+'/enable',{method:'PUT',headers,body:JSON.stringify({enabled:true})});
  if(!enabled.ok)throw new Error('Default model fixture enable failed');
  return String(model.id);
 });
 const setup=page.getByRole('region',{name:'使用授权账户扫描',exact:true});
 await setup.getByRole('link',{name:'添加授权账户模型',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'添加模型',exact:true});
 await dialog.getByRole('button',{name:'保存并配置扫描',exact:true}).waitFor();
 assert.equal(await dialog.getByPlaceholder('sk-…').count(),0);
 assert.equal(await dialog.getByRole('button',{name:'获取列表',exact:true}).count(),0);
 await dialog.getByPlaceholder('给这条配置起个名字',{exact:true}).fill('Fixtures / Mock 授权扫描衔接');
 await dialog.getByPlaceholder('模型 ID',{exact:true}).fill('fixture-chatgpt-flow-model');
 await dialog.getByRole('button',{name:'保存并配置扫描',exact:true}).click();
 await page.waitForURL(u=>u.pathname==='/scans/new');
 const query=new URL(page.url()).searchParams;
 const modelId=query.get('modelConfigId'),identityId=query.get('sdkIdentityId');
 assert.ok(modelId&&identityId);
 const scanURL=base+'/scans/new?'+new URLSearchParams({modelConfigId:modelId,sdkIdentityId:identityId,snapshotId});
 await page.goto(scanURL);
 await page.getByLabel('模型配置',{exact:true}).filter({hasText:'Fixtures / Mock 授权扫描衔接'}).waitFor();
 const picker=()=>page.getByRole('region',{name:'扫描执行身份',exact:true});
 await page.getByLabel('SDK 执行身份',{exact:true}).filter({hasText:'Fixtures / Mock 浏览器 ChatGPT'}).waitFor();
 await page.getByLabel('SDK 执行身份',{exact:true}).click();
 assert.equal(await page.getByRole('option',{name:'使用模型配置的 API Key',exact:true}).count(),0);
 await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'创建扫描',exact:true}).waitFor({state:'visible'});
 await page.waitForFunction(()=>!Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='创建扫描')?.disabled);
 await page.screenshot({path:out+'/account-scan-ready-desktop.png',fullPage:true});
 await page.setViewportSize({width:390,height:844});
 await page.screenshot({path:out+'/account-scan-ready-mobile.png',fullPage:true});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'authorized scan mobile overflow');
 await page.setViewportSize({width:1440,height:1000});
 checks.push('authorized-account-creates-openai-responses-model-and-carries-model-and-identity-to-scan');

 let scanPosts=0;
 const observe=request=>{if(request.method()==='POST'&&new URL(request.url()).pathname==='/api/v1/scans')scanPosts++};
 page.on('request',observe);
 const accountFailure=route=>route.fulfill({status:503,json:{message:'Fixtures / Mock 账户状态读取故障'}});
 await page.route('**/api/v1/sdk-identities',accountFailure);
 await page.getByRole('button',{name:'创建扫描',exact:true}).click();
 await picker().getByRole('alert').filter({hasText:'账户状态读取失败'}).waitFor();
 assert.equal(scanPosts,0);
 assert.ok(await page.getByRole('button',{name:'创建扫描',exact:true}).isDisabled());
 await page.unroute('**/api/v1/sdk-identities',accountFailure);
 await picker().getByRole('button',{name:'刷新账户列表',exact:true}).click();
 await page.waitForFunction(()=>!Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='创建扫描')?.disabled);
 checks.push('submit-rechecks-account-without-model-call-and-read-failure-blocks-task-creation');

 await page.goto(base+'/scans/new?'+new URLSearchParams({modelConfigId:modelId,snapshotId}));
 await picker().getByRole('alert').filter({hasText:'请选择已登录的账户'}).waitFor();
 assert.ok(await page.getByRole('button',{name:'创建扫描',exact:true}).isDisabled());
 assert.ok(await page.getByRole('button',{name:'仅执行 SDK 预检',exact:true}).isDisabled());
 await page.goto(scanURL.replace('sdkIdentityId='+identityId,'sdkIdentityId=999999999'));
 await picker().getByRole('alert').filter({hasText:'选定账户未登录'}).waitFor();
 assert.ok(await page.getByRole('button',{name:'创建扫描',exact:true}).isDisabled());
 await page.goto(scanURL.replace('modelConfigId='+modelId,'modelConfigId=999999999'));
 await page.getByRole('alert').filter({hasText:'链接中的模型配置已不存在'}).waitFor();
 assert.ok(await page.getByRole('button',{name:'创建扫描',exact:true}).isDisabled());
 await page.goto(scanURL.replace('modelConfigId='+modelId,'modelConfigId='+defaultModel));
 await picker().getByRole('alert').filter({hasText:'所选模型不支持此授权账户'}).waitFor();
 assert.ok(await page.getByRole('button',{name:'创建扫描',exact:true}).isDisabled());
 await picker().getByRole('button',{name:'使用模型配置的 API Key',exact:true}).click();
 await page.waitForFunction(()=>!Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='创建扫描')?.disabled);
 assert.equal(scanPosts,0);
 page.off('request',observe);
 checks.push('missing-account-unavailable-account-and-deleted-model-block-without-credential-fallback');

 await page.goto(base+'/models?tab=accounts&identity='+identityId);
 await page.getByRole('region',{name:'使用授权账户扫描',exact:true}).getByLabel('授权账户扫描模型',{exact:true}).click();
 await page.getByRole('option',{name:'Fixtures / Mock 授权扫描衔接 · fixture-chatgpt-flow-model',exact:true}).click();
 await page.getByRole('button',{name:'使用此账户新建扫描',exact:true}).click();
 await page.waitForURL(u=>u.pathname==='/scans/new');
 assert.equal(new URL(page.url()).searchParams.get('modelConfigId'),modelId);
 assert.equal(new URL(page.url()).searchParams.get('sdkIdentityId'),identityId);
 const enabledIds=await page.evaluate(async()=>{
  const response=await fetch('/api/v1/model-configs',{headers:{Authorization:'Bearer '+localStorage.getItem('axiom_token')}});
  return (await response.json()).data.filter(model=>model.enabled).map(model=>String(model.id));
 });
 assert.deepEqual(enabledIds,[defaultModel]);
 checks.push('authenticated-account-can-select-existing-model-without-changing-global-default');
}
