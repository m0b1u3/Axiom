<script setup lang="ts">
import {computed} from 'vue';
import {useRoute,useRouter} from 'vue-router';
import ModelConfigs from '../views/ModelConfigs.vue';
import SDKIdentities from './SDKIdentities.vue';
import PageHeader from './PageHeader.vue';
const route=useRoute(),router=useRouter();
const tab=computed(()=>route.query.tab==='accounts'?'accounts':'models');
function select(value:string){const {setup,...query}=route.query;void router.replace({query:{...query,tab:value}})}
</script>

<template>
 <section aria-label="模型与授权工作区" class="models-authorization">
  <PageHeader title="模型与授权" description="管理扫描模型、连接参数与 API Key / ChatGPT 授权账户。"/>
  <nav class="model-tabs" aria-label="模型与授权分类">
   <button type="button" :aria-current="tab==='models'?'page':undefined" @click="select('models')">模型配置</button>
   <button type="button" :aria-current="tab==='accounts'?'page':undefined" @click="select('accounts')">授权账户</button>
  </nav>
  <ModelConfigs v-if="tab==='models'" embedded/>
  <SDKIdentities v-else :identity-id="typeof route.query.identity==='string'?route.query.identity:undefined" :agent-id="typeof route.query.agent==='string'?route.query.agent:undefined"/>
 </section>
</template>

<style scoped>
.models-authorization{min-width:0}.model-tabs{display:flex;gap:var(--space-5);border-bottom:1px solid var(--border-default);margin-bottom:var(--space-5)}.model-tabs button{padding:var(--space-2) 0;border:0;border-bottom:2px solid transparent;color:var(--text-secondary);font:inherit;font-size:var(--font-size-sm);background:none;cursor:pointer}.model-tabs button[aria-current]{border-bottom-color:var(--accent-primary);color:var(--text-primary);font-weight:var(--font-weight-medium)}.model-tabs button:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}
</style>
