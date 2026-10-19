<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue';
import {api} from '../api';import type {SDKIdentityRef,SDKIdentitySnapshot} from '../contracts';
import {checkedIdentities,identityState,usableIdentity} from '../utils/sdk-identities';import Button from './ui/Button.vue';
import {absoluteTime} from '../utils/time';
const props=defineProps<{identity:SDKIdentityRef}>();const current=ref<SDKIdentitySnapshot>(),error=ref('');let done=false,timer:ReturnType<typeof setTimeout>|undefined;
const reason=computed(()=>!current.value?'当前身份状态尚未确认。':current.value.identity.generation!==props.identity.generation?'凭据版本已改变；此任务不会改用新凭据，请在 Web 取消或另建任务。':!usableIdentity(current.value)?'此身份当前不可用，已阻止新领取；运行任务按取消请求停止。':'使用冻结的身份和绑定节点，不会回退到模型配置或其他账户。');
async function load(){try{const values=checkedIdentities(await api.sdkIdentities());if(!done){current.value=values.find(v=>v.identity.id===props.identity.id);error.value=''}}catch(e){if(!done)error.value=(e as Error).message}}
async function tick(){await load();if(!done)timer=setTimeout(tick,5000)}onMounted(tick);onUnmounted(()=>{done=true;clearTimeout(timer)});
</script>
<template><section class="identity-binding" aria-label="冻结的 SDK 执行身份"><strong>SDK 执行身份</strong><span>{{identity.name}} · {{identity.kind==='chatgpt'?'ChatGPT':'API Key'}} · {{identity.agentId}} · 凭据版本 {{identity.generation}}</span><RouterLink :to="{path:'/settings',query:{section:'models',tab:'accounts',identity:identity.id}}">查看授权状态</RouterLink><p>{{current?'最近记录：'+identityState[current.state]+'；SDK 检查：'+(current.checkedAt?absoluteTime(current.checkedAt):'尚未检查')+'。':''}}{{reason}}</p><p v-if="error" role="alert" class="request-error">身份状态读取失败：{{error}}<Button variant="ghost" size="sm" @click="load">重读身份状态</Button></p></section></template>
<style scoped>.identity-binding{display:flex;flex-wrap:wrap;gap:var(--space-2);font-size:var(--font-size-xs);padding:var(--space-3) 0;border-bottom:1px solid var(--border-default);overflow-wrap:anywhere}.identity-binding p{width:100%;margin:0;color:var(--text-secondary)}</style>
