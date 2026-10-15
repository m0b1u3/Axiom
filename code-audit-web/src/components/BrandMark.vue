<script setup lang="ts">
import symbol from "../assets/axiom-symbol.svg";
defineProps<{ compact?: boolean }>();
</script>
<template><span class="brand-mark"><img :src="symbol" alt="" /><span v-if="!compact"><b>AXIOM</b><small>Security Console</small></span></span></template>
<style scoped>
.brand-mark { display:inline-flex; align-items:center; gap:var(--space-3); }
img { width:42px; height:42px; flex:none; }
b { display:block; font-size:20px; font-weight:650; letter-spacing:.065em; line-height:1.3; color:var(--text-primary); }
small { display:block; margin-top:var(--space-1); font-size:var(--font-size-xs); color:var(--text-tertiary); letter-spacing:.01em; }
</style>
