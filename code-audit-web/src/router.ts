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
          path: "projects/:id",
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
        {
          path: "assets",
          name: "assets",
          component: () => import("./views/Assets.vue"),
          meta: { title: "资产", description: "全部代码快照的统一管理" },
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
          component: () => import("./views/ModelConfigs.vue"),
          meta: { title: "模型配置", description: "管理模型供应商与 API Key" },
        },
        {
          path: "scans/new",
          name: "new-scan",
          component: () => import("./views/NewScan.vue"),
          meta: { title: "新建扫描", description: "上传源码包生成不可变快照" },
        },
        {
          path: "scans/:id",
          name: "scan-detail",
          component: () => import("./views/ScanDetail.vue"),
          meta: { title: "扫描详情", description: "进度、漏洞与报告" },
        },
      ],
    },
  ],
});

router.beforeEach((to) => {
  if (to.name !== "login" && !getToken()) {
    return { name: "login", query: { redirect: to.fullPath } };
  }
});
