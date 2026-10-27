<script setup lang="ts">
import { TooltipProvider } from "reka-ui";
import { Toaster } from "vue-sonner";
import { useTheme } from "./composables/theme";
const { isDark } = useTheme();
import ConfirmHost from "./components/ui/ConfirmHost.vue";
import PageLoadFailure from "./components/PageLoadFailure.vue";
</script>

<template>
  <TooltipProvider :delay-duration="350">
    <PageLoadFailure />
    <router-view />
    <ConfirmHost />
    <Toaster :theme="isDark ? 'dark' : 'light'" position="top-center" :gap="8" :toast-options="{ style: { fontSize: '13px' } }" />
  </TooltipProvider>
</template>

<style>
@media(max-width:600px){
 [data-sonner-toaster][data-x-position="center"]{width:calc(100% - 32px);max-width:calc(100% - 32px)}
}
</style>
