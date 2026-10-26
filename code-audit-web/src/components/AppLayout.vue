<script setup lang="ts">
import { computed, ref, watch, nextTick, onMounted, onUnmounted } from "vue";
import { LayoutDashboard, FolderTree, FolderGit2, ScanLine, ShieldAlert, Archive, Cpu, Settings, Sun, Moon, ChevronRight, KeyRound, LogOut, Search, Menu, X, ArrowUpRight } from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";
import { api, clearToken, type SnapshotInfo } from "../api";
import type { Project, FindingSummary, ScanTask } from "../types";
import BrandMark from "./BrandMark.vue";
import DropdownMenu from "./ui/DropdownMenu.vue";
import Dialog from "./ui/Dialog.vue";
import Input from "./ui/Input.vue";
import ChangePasswordDialog from "./ChangePasswordDialog.vue";
import { useTheme } from "../composables/theme";
import { useAuth } from '../composables/auth';
import Button from './ui/Button.vue';
const route = useRoute(), router = useRouter();
const workspaceKey = computed(() => ['settings','assets','findings','project-detail','knowledge'].includes(String(route.name)) ? route.path : route.fullPath);
const { isDark, toggleTheme } = useTheme();
const {user: identity, roleLabel, refresh: refreshIdentity, error: identityError, loading: identityLoading} = useAuth();
const user = computed(() => identity.value?.username ?? '身份未确认');
const logoutError = ref(''), loggingOut = ref(false);
const mobileOpen = ref(false), passwordOpen = ref(false), searchOpen = ref(false), search = ref("");
const sidebar = ref<HTMLElement | null>(null), menuToggle = ref<HTMLButtonElement | null>(null);
const narrow = ref(window.innerWidth <= 760);
function resizeNavigation(): void { narrow.value = window.innerWidth <= 760; if (!narrow.value) mobileOpen.value = false; }
watch(mobileOpen, async open => { await nextTick(); if (open) sidebar.value?.querySelector<HTMLElement>('a,button')?.focus(); else if (narrow.value) menuToggle.value?.focus(); });
const searchLoading = ref(false), searchError = ref("");
const projects = ref<Project[]>([]), findings = ref<FindingSummary[]>([]), scans = ref<ScanTask[]>([]), assets = ref<SnapshotInfo[]>([]);
let searchTimer:ReturnType<typeof setTimeout> | undefined;
let searchGeneration = 0;
const groups = [
 { label: "工作区", items: [{to:"/",label:"概览",icon:LayoutDashboard},{to:"/projects",label:"项目",icon:FolderGit2},{to:"/scans",label:"扫描任务",icon:ScanLine}] },
 { label:"安全",items:[{to:"/findings",label:"漏洞",icon:ShieldAlert},{to:"/assets",label:"资产",icon:Archive},{to:"/component-plans",label:"组件计划",icon:FolderTree},{to:"/knowledge",label:"知识文档",icon:Archive}] },
 { label:"管理",items:[{to:"/models",label:"模型与授权",icon:Cpu},{to:"/settings",label:"系统设置",icon:Settings}] },
];
const allNav = groups.flatMap(g => g.items);
function isActive(to:string):boolean {const matches = (t:string) => t==='/'?route.path==='/':route.path===t||route.path.startsWith(t+'/');return matches(to)&&!allNav.some(n=>n.to.length>to.length&&matches(n.to)); }
const searchResults = computed(() => {
 const q=search.value.trim().toLowerCase();
 return [
  ...[...allNav.map(n=>({label:n.label,detail:"页面",to:n.to})),...projects.value.map(p=>({label:p.name,detail:"项目",to:`/projects/${p.id}`}))].filter(r=>!q||(r.label+' '+r.detail).toLowerCase().includes(q)),
  ...scans.value.map(s=>({label:s.scanNo,detail:`扫描 · ${s.projectName||s.model}`,to:`/scans/${s.id}`})),
  ...findings.value.map(f=>({label:f.title,detail:`漏洞 · ${f.filePath}`,to:`/scans/${f.scanId}?finding=${f.id}`})),
  ...assets.value.map(s=>({label:s.originalFilename,detail:`代码快照 · #${s.id}`,to:`/assets?q=${encodeURIComponent(s.archiveSha256)}`})),
 ].slice(0,20);
});
async function loadSearch():Promise<void> {
 searchLoading.value=true;searchError.value="";const gen=++searchGeneration;
 const q=search.value.trim();
 const results=await Promise.allSettled([api.listProjects(),api.scanPage(1,q,''),api.findingPage({q,pageSize:10}),api.snapshotPage({q,pageSize:10})]);
 if(gen!==searchGeneration)return;
 projects.value=results[0].status==='fulfilled'?results[0].value:[];
 scans.value=results[1].status==='fulfilled'?results[1].value.items:[];
 findings.value=results[2].status==='fulfilled'?results[2].value.items:[];
 assets.value=results[3].status==='fulfilled'?results[3].value.items:[];
 if(results.some(r=>r.status==='rejected'))searchError.value="部分搜索数据未能加载，仍可使用页面导航。";
 searchLoading.value=false;
}
async function openSearch():Promise<void> {
 searchOpen.value=true;search.value='';clearTimeout(searchTimer);await loadSearch();
}
watch(search,()=>{
 if(!searchOpen.value)return;
 searchGeneration++;searchLoading.value=true;clearTimeout(searchTimer);
 searchTimer=setTimeout(()=>void loadSearch(),250);
});
watch(searchOpen,open=>{if(!open){searchGeneration++;clearTimeout(searchTimer);}});
function navigate(to:string):void {searchOpen.value=false;mobileOpen.value=false;void router.push(to);}
async function logout():Promise<void> {
 if(loggingOut.value)return;
 loggingOut.value=true;logoutError.value='';
 try{await api.logout();clearToken();await router.push({name:'login'});}
 catch(e){logoutError.value=`退出未完成：${(e as Error).message}`;}
 finally{loggingOut.value=false;}
}
function onUserMenu(key:string):void {if(key==='password')passwordOpen.value=true;else if(key==='logout')void logout();}
const userMenu=[{key:"password",label:"修改密码",icon:KeyRound},{key:"logout",label:"退出登录",icon:LogOut}];
function shortcuts(e:KeyboardEvent):void {
 if((e.metaKey||e.ctrlKey)&&e.key.toLowerCase()==='k'){e.preventDefault();void openSearch();}
 if(e.key==='Escape')mobileOpen.value=false;
 if(mobileOpen.value && e.key==='Tab' && !searchOpen.value && !passwordOpen.value){
  const controls = [...(sidebar.value?.querySelectorAll<HTMLElement>('a[href],button:not([disabled])') ?? [])].filter(el=>el.offsetParent!==null);
  const first=controls[0], last=controls.at(-1);
  if(e.shiftKey && document.activeElement===first){e.preventDefault();last?.focus();}
  else if(!e.shiftKey && document.activeElement===last){e.preventDefault();first?.focus();}
 }
}
watch(()=>route.fullPath,()=>{mobileOpen.value=false;});
onMounted(()=>{window.addEventListener('keydown',shortcuts);window.addEventListener('resize',resizeNavigation);void refreshIdentity();});
onUnmounted(()=>{searchGeneration++;clearTimeout(searchTimer);window.removeEventListener('keydown',shortcuts);window.removeEventListener('resize',resizeNavigation);});
</script>
<template>
 <div class="shell">
  <a class="skip-link" href="#main-content">跳至主要内容</a>
  <button v-if="mobileOpen" class="nav-backdrop" aria-label="关闭导航" @click="mobileOpen=false" />
  <aside ref="sidebar" id="application-navigation" class="sidebar" :class="{'mobile-open':mobileOpen}" :inert="narrow && !mobileOpen" :aria-hidden="narrow && !mobileOpen ? true : undefined" aria-label="应用导航">
   <router-link to="/" class="brand" aria-label="Axiom 首页"><BrandMark /></router-link>
   <nav aria-label="主要导航"><section v-for="group in groups" :key="group.label" class="nav-group"><div class="group-label">{{group.label}}</div><router-link v-for="item in group.items" :key="item.to" :to="item.to" class="nav-item" :class="{active:isActive(item.to)}" :aria-current="isActive(item.to)?'page':undefined"><component :is="item.icon" :size="16" :stroke-width="1.6"/><span>{{item.label}}</span></router-link></section></nav>
   <div class="bottom"><DropdownMenu :items="userMenu" @select="onUserMenu"><button class="account" aria-label="账户菜单"><span class="avatar">{{user.slice(0,1).toUpperCase()}}</span><span class="user-meta"><b>{{user}}</b><small>{{roleLabel}}</small></span><ChevronRight :size="14"/></button></DropdownMenu><button class="theme-button" @click="toggleTheme" :aria-label="isDark?'切换浅色模式':'切换深色模式'"><Sun v-if="isDark" :size="16"/><Moon v-else :size="16"/><span>{{isDark?'浅色模式':'深色模式'}}</span><span class="theme-track" :class="{on:isDark}"/></button></div>
  </aside>
  <div class="workspace" :inert="narrow && mobileOpen"><header class="workspace-topbar"><div class="breadcrumb"><button ref="menuToggle" class="mobile-menu" aria-controls="application-navigation" @click="mobileOpen=!mobileOpen" :aria-expanded="mobileOpen" aria-label="切换导航"><X v-if="mobileOpen" :size="18"/><Menu v-else :size="18"/></button><span class="workspace-label">工作空间</span><span class="slash">/</span><strong>{{route.meta.title||'概览'}}</strong></div><div class="header-actions"><button class="search-trigger" @click="openSearch" aria-label="搜索项目、扫描和漏洞"><Search :size="14"/><span>搜索项目、扫描、漏洞…</span><kbd>⌘ K</kbd></button><span class="header-avatar" aria-hidden="true">{{user.slice(0,1).toUpperCase()}}</span></div></header><main id="main-content" class="content" tabindex="-1"><div v-if="logoutError" class="request-error" role="alert"><span>{{logoutError}}</span><Button variant="outline" size="sm" :loading="loggingOut" @click="logout">重试退出</Button></div><router-view v-if="identity" :key="workspaceKey" /><p v-else-if="identityLoading" role="status">正在确认登录身份…</p><div v-else class="request-error" role="alert"><span>无法确认登录身份：{{identityError || '请重新登录'}}</span><Button variant="outline" size="sm" @click="refreshIdentity">重试</Button></div></main></div>
  <ChangePasswordDialog v-model:open="passwordOpen"/>
  <Dialog v-model:open="searchOpen" title="搜索工作空间" description="查找项目、扫描记录和漏洞，或前往功能页面。" width="560px"><Input v-model="search" placeholder="输入名称、扫描编号或文件路径" aria-label="搜索工作空间"/><p v-if="searchLoading" class="search-status" role="status">正在加载搜索数据…</p><p v-if="searchError" class="search-status" role="alert">{{searchError}}</p><p v-if="search.trim()" class="search-status">显示部分匹配。<button @click="navigate('/findings?q='+encodeURIComponent(search.trim()))">查看全部漏洞</button> · <button @click="navigate('/assets?q='+encodeURIComponent(search.trim()))">查看全部资产</button></p><div class="search-results"><button v-for="(result,i) in searchResults" :key="result.to+'-'+i" class="search-result" @click="navigate(result.to)"><span>{{result.label}}<small>{{result.detail}}</small></span><ArrowUpRight :size="14"/></button><p v-if="!searchResults.length&&!searchLoading" class="search-status">没有匹配结果，请尝试其他关键词。</p></div></Dialog>
 </div>
</template>
<style scoped>
.shell{min-height:100dvh;display:grid;grid-template-columns:var(--sidebar-width) minmax(0,1fr)}
.sidebar{height:100dvh;position:sticky;top:0;background:var(--surface-sidebar);border-right:1px solid var(--border-default);display:flex;flex-direction:column;padding:var(--space-8) var(--space-3) var(--space-4);z-index:var(--z-sidebar)}
.brand{display:block;padding:0 var(--space-3);margin-bottom:var(--space-8)}
.nav-group+.nav-group{margin-top:var(--space-6)}.group-label{font-size:var(--font-size-2xs);color:var(--text-tertiary);padding:0 var(--space-3);margin-bottom:var(--space-2)}
.nav-item{display:flex;gap:var(--space-3);align-items:center;padding:var(--space-2-5) var(--space-3);margin:var(--space-0-5) 0;border-radius:var(--radius-md);font-size:var(--font-size-sm);color:var(--text-secondary);transition:background var(--duration-fast),color var(--duration-fast)}.nav-item:hover{background:var(--surface-hover);color:var(--text-primary)}.nav-item.active{background:var(--accent-subtle);color:var(--accent-primary)}
.bottom{margin-top:auto;padding-top:var(--space-6)}.account{display:flex;align-items:center;gap:var(--space-3);width:100%;border:1px solid var(--border-default);background:var(--surface-primary);border-radius:var(--radius-lg);padding:var(--space-2-5)}.avatar,.header-avatar{width:30px;height:30px;border-radius:50%;display:grid;place-items:center;background:var(--surface-tertiary);font-size:var(--font-size-xs);color:var(--text-secondary);flex:none}.user-meta{flex:1;min-width:0;text-align:left}.user-meta b{display:block;font-size:var(--font-size-sm);font-weight:var(--font-weight-medium);overflow:hidden;text-overflow:ellipsis}.user-meta small{display:block;font-size:var(--font-size-2xs);margin-top:var(--space-1);color:var(--text-tertiary)}.account>svg{color:var(--text-tertiary)}
.theme-button{display:flex;align-items:center;gap:var(--space-3);width:100%;padding:var(--space-4) var(--space-3) 0;font-size:var(--font-size-xs);color:var(--text-secondary)}.theme-track{margin-left:auto;width:26px;height:15px;border-radius:var(--radius-xl);background:var(--border-strong);position:relative}.theme-track:after{content:'';width:9px;height:9px;border-radius:50%;background:white;position:absolute;left:3px;top:3px;transition:transform var(--duration-default)}.theme-track.on{background:var(--accent-primary)}.theme-track.on:after{transform:translateX(11px)}
.workspace{min-width:0}.workspace-topbar{height:var(--header-height);display:flex;align-items:center;justify-content:space-between;padding:0 var(--space-8);background:var(--surface-primary);border-bottom:1px solid var(--border-default)}.breadcrumb{display:flex;gap:var(--space-3);align-items:center;font-size:var(--font-size-xs);color:var(--text-tertiary)}.breadcrumb strong{font-weight:var(--font-weight-medium);color:var(--text-primary)}.slash{color:var(--text-disabled)}.header-actions{display:flex;align-items:center;gap:var(--space-6)}.search-trigger{display:flex;align-items:center;gap:var(--space-2);width:260px;border:1px solid var(--border-default);background:var(--surface-app);padding:var(--space-2) var(--space-3);border-radius:var(--radius-md);color:var(--text-tertiary);font-size:var(--font-size-xs)}.search-trigger:hover{border-color:var(--border-strong)}kbd{margin-left:auto;border:1px solid var(--border-default);border-radius:var(--radius-xs);font-family:inherit;font-size:var(--font-size-2xs);padding:0 var(--space-1)}.header-avatar{width:28px;height:28px}.content{max-width:var(--content-max-width);padding:var(--space-9) var(--space-8) var(--space-8);margin:auto;outline:none;min-width:0}.mobile-menu{display:none}.search-results{margin-top:var(--space-3)}.search-result{display:flex;align-items:center;justify-content:space-between;gap:var(--space-3);width:100%;text-align:left;padding:var(--space-3);border-radius:var(--radius-sm);font-size:var(--font-size-sm)}.search-result:hover{background:var(--surface-hover)}.search-result>span{min-width:0;overflow-wrap:anywhere}.search-result small{display:block;font-size:var(--font-size-xs);color:var(--text-tertiary);margin-top:var(--space-1)}.search-status{font-size:var(--font-size-sm);color:var(--text-secondary);padding:var(--space-3)}.skip-link{position:fixed;top:-100px;left:var(--space-4);z-index:200;background:var(--surface-primary);padding:var(--space-3);border:1px solid var(--accent-primary)}.skip-link:focus{top:var(--space-3)}.nav-backdrop{display:none}
@media(max-width:1100px){.content{padding:var(--space-6)}.workspace-topbar{padding:0 var(--space-6)}}
@media(max-width:760px){.shell{display:block}.sidebar{position:fixed;left:0;width:var(--sidebar-width);transform:translateX(-100%);transition:transform var(--duration-default);z-index:var(--z-dropdown)}.sidebar.mobile-open{transform:translateX(0)}.nav-backdrop{display:block;position:fixed;inset:0;background:#080e1b66;z-index:var(--z-inspector)}.workspace-topbar{height:56px;padding:0 var(--space-4)}.mobile-menu{display:inline-flex;align-items:center;padding:var(--space-1)}.workspace-label,.slash{display:none}.search-trigger{width:auto;border:0;background:none;padding:var(--space-2)}.search-trigger span,.search-trigger kbd,.header-avatar{display:none}.content{padding:var(--space-6) var(--space-4)}.breadcrumb{gap:var(--space-2)}}
@media(prefers-reduced-motion:reduce){.sidebar,.theme-track:after{transition:none}}
</style>
