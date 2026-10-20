import assert from 'node:assert/strict';
import path from 'node:path';

// Measure actual rendered SVG shapes and glyphs, not the layout helper's output.
export async function graphGeometry(region){
 return region.locator('.evidence-canvas').evaluate(canvas=>{
  const svg=canvas.querySelector('svg'),origin=svg.getBoundingClientRect();
  const box=el=>{const r=el.getBoundingClientRect();return {x:r.left-origin.left,y:r.top-origin.top,width:r.width,height:r.height};};
  const nodes=Array.from(svg.querySelectorAll('path')).filter(p=>{const r=p.getBoundingClientRect();return p.getAttribute('fill')&&p.getAttribute('fill')!=='none'&&r.width>=40&&r.height>=20;}).map(box);
  const texts=Array.from(svg.querySelectorAll('text')).filter(t=>t.textContent?.trim()).map(t=>({text:t.textContent,...box(t)}));
  return {width:origin.width,height:origin.height,clientHeight:canvas.clientHeight,inlineHeight:canvas.style.height,svgHeight:svg.getAttribute('height'),nodes,texts,overflow:canvas.scrollWidth>canvas.clientWidth};
 });
}

export function assertGeometry(g,count,{contained=true}={}){
 const epsilon=2;
 assert.equal(g.nodes.length,count,'every graph node must have a rendered rectangle');
 assert.equal(g.overflow,false,'graph must not widen its container');
 for(const n of g.nodes)if(contained)assert.ok(n.x>=-epsilon&&n.y>=-epsilon&&n.x+n.width<=g.width+epsilon&&n.y+n.height<=g.height+epsilon,JSON.stringify({clipped:n,width:g.width,height:g.height}));
 for(let i=0;i<g.nodes.length;i++)for(let j=i+1;j<g.nodes.length;j++){
  const a=g.nodes[i],b=g.nodes[j];
  assert.ok(a.x+a.width<=b.x+epsilon||b.x+b.width<=a.x+epsilon||a.y+a.height<=b.y+epsilon||b.y+b.height<=a.y+epsilon,JSON.stringify({overlap:[a,b],width:g.width}));
 }
 const relationLabels=new Set(['结果包含','SDK 引用支持','SDK 描述顺序','SDK 报告反证','受约束','引用']);
 const labels=g.texts.filter(t=>!relationLabels.has(t.text));
 assert.ok(labels.length>=count,'each node must retain its visible label');
 for(const t of labels)assert.equal(g.nodes.filter(n=>t.x>=n.x-epsilon&&t.y>=n.y-epsilon&&t.x+t.width<=n.x+n.width+epsilon&&t.y+t.height<=n.y+n.height+epsilon).length,1,JSON.stringify({unboundedLabel:t,width:g.width}));
}

async function settled(page){await page.evaluate(async()=>{await document.fonts.ready;for(let i=0;i<3;i++)await new Promise(r=>requestAnimationFrame(r));});}
// ResizeObserver schedules the chart update on another animation frame. Poll
// the rendered SVG with a bounded deadline instead of assuming a frame count.
export async function waitForEvidenceGeometry(page,region,count,failures){
 const deadline=Date.now()+1500;let failure,last;
 do{await settled(page);last=await graphGeometry(region);try{assertGeometry(last,count);return last;}catch(e){failure=e;}}while(Date.now()<deadline);
 if(!failures){failure.geometry=last;throw failure;}
 failures.push({error:failure.message,width:last.width,height:last.height,clientHeight:last.clientHeight,inlineHeight:last.inlineHeight,svgHeight:last.svgHeight});return last;
}
async function shot(page,region,out,name){if(!out)return;await settled(page);await region.screenshot({path:path.join(out,name+'.png'),animations:'disabled'});}
const samePositions=(a,b)=>{assert.equal(a.nodes.length,b.nodes.length);for(let i=0;i<a.nodes.length;i++)for(const key of ['x','y','width','height'])assert.ok(Math.abs(a.nodes[i][key]-b.nodes[i][key])<.2,`selection changed graph ${key}`);};

export async function exerciseEvidenceLayout(page,region,graph,{screenshots}={}){
 const checks=[],measurements=[],geometryFailures=[],originalViewport=page.viewportSize(),originalStyle=await region.getAttribute('style');
 const checkFit=count=>waitForEvidenceGeometry(page,region,count,geometryFailures);
 const code=graph.nodes.find(n=>n.sdkEvidenceId==='E-source'),review=graph.nodes.find(n=>n.kind==='validation');
 const locator=region.getByLabel('定位节点或关系',{exact:true}),inspector=region.getByRole('region',{name:'选中证据详情',exact:true});
 await region.getByRole('button',{name:'证据关系',exact:true}).click();await region.getByRole('button',{name:'图谱',exact:true}).click();await locator.selectOption(code.id);
 try{
  await page.setViewportSize({width:1920,height:1000});
  for(const width of [960,786,700,550,480,360,240]){
   await region.evaluate((el,width)=>{el.style.width=width+'px';el.style.maxWidth='100%';},width);
   const g=await checkFit(graph.nodes.length);
   await inspector.getByRole('heading',{name:code.label,exact:true}).waitFor();
   assert.equal(page.viewportSize().width,1920,'container test must not resize the browser window');
   measurements.push({requestedWidth:width,canvasWidth:g.width,canvasHeight:g.height,columns:new Set(g.nodes.map(n=>Math.round(n.x))).size,nodes:g.nodes.length});
   if(width===786)await shot(page,region,screenshots,'layout-desktop');
   if(width===550)await shot(page,region,screenshots,'layout-narrow');
  }
  assert.equal(measurements.at(-1).columns,1);assert.equal(measurements[0].columns,4);
  assert.ok(measurements.some(m=>m.columns===2)&&measurements.some(m=>m.columns===3));
  checks.push('actual-svg-nodes-and-labels-fit-one-to-four-columns-with-container-only-resize');
  await region.evaluate(el=>{el.style.width='550px';});await checkFit(graph.nodes.length);
  await region.getByRole('button',{name:'攻击路径',exact:true}).click();
  const pathCount=await locator.locator('option').count()-1; // All path relations are also in this select.
  assert.ok(pathCount>0);const pathNodes=graph.nodes.filter(n=>['source','step','control','sink','impact','assumption','limitation'].includes(n.kind)).length;
  await checkFit(pathNodes);await region.getByRole('button',{name:'证据关系',exact:true}).click();await locator.selectOption(code.id);
  checks.push('path-filter-reflows-without-dropping-node-or-relation-inspection');
  await region.getByRole('button',{name:'适应画布',exact:true}).click();const unzoomed=await checkFit(graph.nodes.length);
  await region.getByRole('button',{name:'缩小证据图谱',exact:true}).click();const zoomed=await checkFit(graph.nodes.length);
  assert.ok(zoomed.nodes[0].width<unzoomed.nodes[0].width);
  await locator.selectOption(review.id);await settled(page);samePositions(zoomed,await graphGeometry(region));
  await inspector.getByText('推断 · SDK 复核',{exact:true}).waitFor();
  const wheelCanvas=region.locator('.evidence-canvas');await wheelCanvas.scrollIntoViewIfNeeded();const wheelBounds=await wheelCanvas.boundingBox();
  await page.mouse.move(wheelBounds.x+wheelBounds.width/2,wheelBounds.y+wheelBounds.height/2);await page.mouse.wheel(0,80);await settled(page);const wheelZoomed=await checkFit(graph.nodes.length);assert.ok(wheelZoomed.nodes[0].width<zoomed.nodes[0].width,'wheel must zoom nodes and labels together');
  await region.getByRole('button',{name:'适应画布',exact:true}).click();await checkFit(graph.nodes.length);
  // A small drag in empty graph space tests the real roam handler, then a
  // selection verifies that rendering the inspector does not reset the view.
  const canvas=region.locator('.evidence-canvas');await canvas.scrollIntoViewIfNeeded();
  const local=await graphGeometry(region),bounds=await canvas.boundingBox();
  const a=local.nodes[0],b=local.nodes.find(n=>n.y>a.y+a.height);
  const start={x:bounds.x+local.width/2,y:bounds.y+(a.y+a.height+b.y)/2};
  await page.mouse.move(start.x,start.y);await page.mouse.down();await page.mouse.move(start.x+12,start.y+12,{steps:4});await page.mouse.up();await settled(page);
  const panned=await graphGeometry(region);assert.ok(Math.abs(panned.nodes[0].x-local.nodes[0].x)>2||Math.abs(panned.nodes[0].y-local.nodes[0].y)>2,'drag must pan the graph');
  await locator.selectOption(code.id);await settled(page);samePositions(panned,await graphGeometry(region));
  await region.getByRole('button',{name:'适应画布',exact:true}).click();await checkFit(graph.nodes.length);
  checks.push('zoom-and-pan-remain-stable-when-selecting-a-node-and-fit-restores-readable-bounds');
  const support=graph.edges.find(e=>e.kind==='supports'&&e.from===code.id);await locator.selectOption(support.id);
  await inspector.getByRole('button',{name:'起点：'+code.label,exact:true}).click();await inspector.getByText('/raw/codeEvidence/0',{exact:true}).waitFor();
  await inspector.getByRole('button',{name:'查看对应原始数据',exact:true}).click();await inspector.getByRole('region',{name:'对应原始证据',exact:true}).waitFor();
  const nav=region.getByRole('group',{name:'证据图谱键盘导航',exact:true});await nav.focus();await page.keyboard.press('Home');await page.keyboard.press('ArrowDown');await page.keyboard.press('Enter');assert.equal(await page.evaluate(()=>document.activeElement?.id),'evidence-selection-title');
  checks.push('narrow-panel-relations-raw-evidence-and-keyboard-investigation-remain-operable');
  await region.evaluate((el,style)=>{if(style===null)el.removeAttribute('style');else el.setAttribute('style',style);},originalStyle);
  for(const width of [634,390]){
   await page.setViewportSize({width,height:900});await region.getByRole('button',{name:'图谱',exact:true}).click();await locator.selectOption(code.id);await checkFit(graph.nodes.length);
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   if(width===634)await shot(page,region,screenshots,'layout-embedded');
   else{await shot(page,region,screenshots,'layout-mobile-graph');await region.getByRole('button',{name:'列表',exact:true}).click();await region.getByRole('list',{name:'证据节点列表',exact:true}).waitFor();await shot(page,region,screenshots,'layout-mobile-list');}
  }
  checks.push('embedded-and-phone-viewports-support-both-graph-and-list-without-page-overflow');
  await page.setViewportSize({width:1440,height:1000});
  async function replaceGraph(value){
   await page.route('**/evidence-graph?*',r=>r.fulfill({status:200,contentType:'application/json',body:JSON.stringify({data:value})}),{times:1});
   await page.reload();await region.getByRole('button',{name:'查看证据链',exact:true}).click();await region.locator('svg').waitFor();
  }
  const long=structuredClone(graph);long.nodes[0].label='Fixtures / Mock：长标签\n包含换行\t'+('不可拆分的源码符号'.repeat(15));
  await replaceGraph(long);await region.evaluate(el=>{el.style.width='550px';el.style.maxWidth='100%';});await checkFit(graph.nodes.length);
  await locator.selectOption(long.nodes[0].id);await inspector.getByRole('button',{name:'查看对应原始数据',exact:true}).click();await inspector.getByRole('region',{name:'对应原始证据',exact:true}).waitFor();
  checks.push('long-and-multiline-labels-stay-inside-nodes-with-raw-evidence-still-accessible');
  const singleton={...graph,nodes:[code],edges:[],completeness:'partial'};await replaceGraph(singleton);
  await region.evaluate(el=>{el.style.width='240px';el.style.maxWidth='100%';});await checkFit(1);await inspector.getByRole('heading',{name:code.label,exact:true}).waitFor();
  await region.evaluate(el=>{el.style.display='none';el.style.width='480px';});await settled(page);await page.locator('.evidence-chain').evaluate(el=>{el.style.removeProperty('display');});await checkFit(1);
  checks.push('single-node-and-hidden-panel-resize-keep-finite-readable-svg-bounds');
 }catch(e){e.geometryFailures=geometryFailures;e.measurements=measurements;throw e;}finally{
  await page.reload();await region.getByRole('button',{name:'查看证据链',exact:true}).click();await region.getByRole('button',{name:'证据关系',exact:true}).waitFor();
  await region.evaluate((el,style)=>{if(style===null)el.removeAttribute('style');else el.setAttribute('style',style);},originalStyle);
  await page.setViewportSize(originalViewport);await region.getByRole('button',{name:'证据关系',exact:true}).click();await region.getByRole('button',{name:'图谱',exact:true}).click();await locator.selectOption(code.id);
 }
 assert.deepEqual(geometryFailures,[],'rendered geometry must remain valid in every responsive scenario');
 return {checks,measurements};
}
