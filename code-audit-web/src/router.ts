import { createRouter, createWebHistory } from "vue-router";
import { getToken } from "./api";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/login",
      name: "login",
      component: () => import("./views/Login.vue"),
    },
    {
      path: "/",
      component: () => import("./components/AppLayout.vue"),
      children: [
        {path:"settings/structured-output",name:"structured-output-diagnostic",component:()=>import("./components/StructuredOutputDiagnostic.vue"),meta:{title:"结构化输出诊断",description:"固定样例与离线请求对照"}},
 {path:"component-plans",name:"component-plans",component:()=>import("./views/ComponentPlans.vue"),meta:{title:"组件计划",description:"编辑、校验和确认不可变源码的组件边界"}},
 {path:"component-plans/:id([1-9]\\d*)",name:"component-plan",component:()=>import("./views/ComponentPlans.vue"),meta:{title:"组件计划",description:"草稿校验与冻结版本"}},
        {
          path: "",
          name: "dashboard",
          component: () => import("./views/Dashboard.vue"),
          meta: { title: "概览", description: "扫描运行状态与最近活动" },
        },
        {
          path: "projects",
          name: "projects",
          component: () => import("./views/Projects.vue"),
          meta: { title: "项目", description: "组织与浏览被审计的代码资产" },
        },
        {
          path: "projects/:id([1-9]\\d*)",
          name: "project-detail",
          component: () => import("./views/ProjectDetail.vue"),
          meta: { title: "项目详情", description: "代码快照与该项目扫描记录" },
        },
        {
          path: "scans",
          name: "scans",
          component: () => import("./views/ScanList.vue"),
          meta: { title: "扫描任务", description: "管理代码安全扫描任务" },
        },
        {
          path: "findings",
          name: "findings",
          component: () => import("./views/Findings.vue"),
          meta: { title: "漏洞", description: "全局风险视图" },
        },
        {path:"knowledge",name:"knowledge",component:()=>import("./views/Knowledge.vue"),meta:{title:"知识文档",description:"管理扫描参考文档与不可变版本"}},
        {
          path: "assets",
          name: "assets",
          component: () => import("./views/Assets.vue"),
          meta: { title: "资产", description: "全部代码快照的统一管理" },
        },
        {
          path: "models",
          name: "models-authorization",
          component: () => import("./components/ModelsAndAuthorization.vue"),
          meta: { title: "模型与授权", description: "管理扫描模型与独立授权账户" },
        },
        {
          path: "settings",
          name: "settings",
          component: () => import("./views/Settings.vue"),
          meta: { title: "系统设置", description: "账户与运行环境" },
        },
        {
          path: "settings/model-configs",
          name: "model-configs",
          redirect: to => {
            const {section: _section, ...query} = to.query;
            return {path:'/models',query:{...query,tab:'models'},hash:to.hash};
          },
        },
        {
          path: "scans/new",
          name: "new-scan",
          component: () => import("./views/NewScan.vue"),
          meta: { title: "新建扫描", description: "上传源码包生成不可变快照" },
        },
        {
          path: "scans/:id([1-9]\\d*)",
          name: "scan-detail",
          component: () => import("./views/ScanDetail.vue"),
          meta: { title: "扫描详情", description: "进度、漏洞与报告" },
        },
        {
          path: ":pathMatch(.*)*",
          name: "not-found",
          component: () => import("./views/NotFound.vue"),
          meta: { title: "页面不存在" },
        },
      ],
    },
  ],
});

router.beforeEach((to) => {
  if (to.name !== "login" && !getToken()) {
    return { name: "login", query: { redirect: to.fullPath } };
  }
  if(to.name==='settings'&&['models','sdk-identities'].includes(String(to.query.section))){
    const {section, ...query}=to.query;
    return {path:'/models',query:{...query,tab:section==='sdk-identities'?'accounts':query.tab==='accounts'?'accounts':'models'},hash:to.hash};
  }
});
