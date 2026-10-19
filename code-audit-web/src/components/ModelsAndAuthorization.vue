<script setup lang="ts">
import {computed} from 'vue';
import {useRoute,useRouter} from 'vue-router';
import ModelConfigs from '../views/ModelConfigs.vue';
import SDKIdentities from './SDKIdentities.vue';
const route=useRoute(),router=useRouter();
const tab=computed(()=>route.query.tab==='accounts'?'accounts':'models');
function select(value:string){void router.replace({query:{...route.query,section:'models',tab:value}})}
</script>

<template>
 <section aria-labelledby="models-authorization-title" class="models-authorization">
  <header><h2 id="models-authorization-title">模型与授权</h2><p>模型配置管理供应商、型号与连接参数。使用独立授权时，在这里登录 API Key 或 ChatGPT，再在新建扫描中选用。</p></header>
  <nav class="model-tabs" aria-label="模型与授权分类">
   <button type="button" :aria-current="tab==='models'?'page':undefined" @click="select('models')">模型配置</button>
   <button type="button" :aria-current="tab==='accounts'?'page':undefined" @click="select('accounts')">授权账户</button>
  </nav>
  <ModelConfigs v-if="tab==='models'" embedded/>
  <SDKIdentities v-else :identity-id="typeof route.query.identity==='string'?route.query.identity:undefined" :agent-id="typeof route.query.agent==='string'?route.query.agent:undefined"/>
 </section>
</template>

<style scoped>
.models-authorization{min-width:0}header h2{margin:0;font-size:var(--font-size-lg);font-weight:var(--font-weight-semibold)}header p{margin:var(--space-2) 0 var(--space-5);max-width:75ch;font-size:var(--font-size-sm);line-height:var(--line-height-body);color:var(--text-secondary)}.model-tabs{display:flex;gap:var(--space-5);border-bottom:1px solid var(--border-default);margin-bottom:var(--space-5)}.model-tabs button{padding:var(--space-2) 0;border:0;border-bottom:2px solid transparent;color:var(--text-secondary);font:inherit;font-size:var(--font-size-sm);background:none;cursor:pointer}.model-tabs button[aria-current]{border-bottom-color:var(--accent-primary);color:var(--text-primary);font-weight:var(--font-weight-medium)}.model-tabs button:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}
</style>
