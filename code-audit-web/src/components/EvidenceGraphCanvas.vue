<script setup lang="ts">
import {onMounted,onUnmounted,ref,watch} from 'vue';
import * as echarts from 'echarts/core';
import {GraphChart,type GraphSeriesOption} from 'echarts/charts';
import {SVGRenderer} from 'echarts/renderers';
import type {EvidenceNode,EvidenceRelation} from '../contracts';
import {evidenceKinds,evidenceRelations,evidenceStates,layoutEvidenceGraph,type GraphView} from '../utils/evidence-graph';
echarts.use([GraphChart,SVGRenderer]);
const props=defineProps<{nodes:EvidenceNode[];edges:EvidenceRelation[];view:GraphView;selected:string}>();
const emit=defineEmits<{select:[kind:'node'|'edge',id:string]}>();
const element=ref<HTMLDivElement|null>(null);let chart:echarts.ECharts|null=null,observer:ResizeObserver|null=null,themeObserver:MutationObserver|null=null,resizeFrame:number|null=null;let previousView=props.view,observedWidth=0,geometry='';
function render(reset=false,requestedZoom?:number){
 if(!element.value||!chart||element.value.clientWidth===0)return;const styles=getComputedStyle(element.value),color=(token:string)=>styles.getPropertyValue(token).trim();
 const previous=(chart.getOption()??{}) as {series?:Array<{zoom?:number;center?:GraphSeriesOption['center']}>};
 if(previousView!==props.view){reset=true;previousView=props.view;}
 const fontSize=Number.parseFloat(styles.getPropertyValue('--font-size-sm'))||12;
 const gap=Number.parseFloat(styles.getPropertyValue('--space-6'))||24,padding=gap;
 const labelPadding=Number.parseFloat(styles.getPropertyValue('--space-2'))||8;
 const lineHeight=fontSize+(Number.parseFloat(styles.getPropertyValue('--space-1'))||4);
 const minHeight=(Number.parseFloat(styles.getPropertyValue('--height-table-row'))||34)*12;
 const layout=layoutEvidenceGraph(props.nodes,props.view,{width:element.value.clientWidth,nodeWidth:fontSize*12.5,nodeHeight:lineHeight*2+labelPadding*2,gap,padding,minHeight});
 const key=[layout.width,layout.height,layout.nodeWidth,layout.nodeHeight,layout.columns].join(':');
 if(key!==geometry){reset=true;geometry=key;}
 observedWidth=element.value.clientWidth;
 element.value.style.height=layout.height+'px';
 chart.resize({width:layout.width,height:layout.height});
 const zoom=reset?1:requestedZoom??previous.series?.[0]?.zoom??1;
 const positions=new Map(layout.positions.map(p=>[p.id,{x:p.x,y:p.y}]));
 const option:GraphSeriesOption={type:'graph',layout:'none',roam:true,nodeScaleRatio:1,zoom,center:reset?undefined:previous.series?.[0]?.center,preserveAspect:false,...layout.bounds,scaleLimit:{min:.35,max:3},animation:false,edgeSymbol:['none','arrow'],edgeSymbolSize:6,
  emphasis:{scale:false,focus:'adjacency'},
  data:props.nodes.map(n=>({id:n.id,name:n.label,...positions.get(n.id),symbol:'rect',symbolSize:[layout.nodeWidth,layout.nodeHeight],itemStyle:{color:color(n.id===props.selected?'--surface-selected':'--surface-primary'),borderColor:color(n.id===props.selected?'--border-focus':'--border-strong'),borderWidth:1},label:{show:true,color:color('--text-primary'),fontFamily:styles.fontFamily,fontSize:fontSize*zoom,lineHeight:lineHeight*zoom,width:Math.max(1,(layout.nodeWidth-labelPadding*2)*zoom),overflow:'truncate',formatter:()=>`${evidenceKinds[n.kind]} · ${n.state==='observed'?'已观察':evidenceStates[n.state]}\n${n.label.replace(/\s+/gu,' ').trim()}`}})),
  links:props.edges.map(e=>({id:e.id,source:e.from,target:e.to,name:evidenceRelations[e.kind],lineStyle:{color:color(e.kind==='contradicts'?'--evidence-contradicted':e.id===props.selected?'--accent-primary':'--text-tertiary'),width:e.id===props.selected?2:1,opacity:!props.selected||e.from===props.selected||e.to===props.selected||e.id===props.selected?1:.85,type:'dashed',curveness:e.from===e.to? .35:.12},label:{show:e.id===props.selected,formatter:()=>evidenceRelations[e.kind],fontSize:(fontSize-1)*zoom,color:color('--text-primary')}}))};
 chart.setOption({series:[option]},true);
}
function fit(){render(true);}
function scale(factor:number){const current=chart?.getOption() as {series?:Array<{zoom?:number}>}|undefined;render(false,Math.max(.35,Math.min(3,(current?.series?.[0]?.zoom??1)*factor)));}
defineExpose({fit,scale});
onMounted(()=>{if(!element.value)return;chart=echarts.init(element.value,undefined,{renderer:'svg'});chart.on('click',params=>{const event=params as unknown as {dataType?:string;data?:{id?:string}};if(event.data?.id)emit('select',event.dataType==='edge'?'edge':'node',event.data.id);});chart.on('graphroam',params=>{if((params as {zoom?:number}).zoom)render();});observer=new ResizeObserver(()=>{if(element.value&&element.value.clientWidth!==observedWidth&&resizeFrame===null)resizeFrame=requestAnimationFrame(()=>{resizeFrame=null;render(true);});});observer.observe(element.value);themeObserver=new MutationObserver(()=>render());themeObserver.observe(document.documentElement,{attributes:true,attributeFilter:['class','style']});render();});
watch(()=>[props.nodes,props.edges,props.selected,props.view],()=>render());
onUnmounted(()=>{observer?.disconnect();themeObserver?.disconnect();if(resizeFrame!==null)cancelAnimationFrame(resizeFrame);chart?.dispose();chart=null;});
</script>
<template><div ref="element" class="evidence-canvas" aria-hidden="true" /></template>
<style scoped>.evidence-canvas{width:100%;height:calc(var(--height-table-row)*12);min-width:0;background:var(--surface-secondary)}</style>
