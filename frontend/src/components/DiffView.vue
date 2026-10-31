<script setup>
defineProps({ lines: { type: Array, default: () => [] }, empty: { type: String, default: '没有差异' } })
</script>

<template>
  <pre v-if="lines.length" class="diff"><div v-for="(l, i) in lines" :key="i" :class="'op-' + (l.op === ' ' ? 'eq' : l.op === '+' ? 'add' : l.op === '-' ? 'del' : 'skip')">{{ l.op === '@' ? l.text : l.op + ' ' + l.text }}</div></pre>
  <el-empty v-else :description="empty" :image-size="50" />
</template>

<style scoped>
.diff { background:var(--el-fill-color-light); border-radius:6px; padding:8px 0; margin:0; max-height:60vh; overflow:auto;
        font:12px/1.55 ui-monospace, SFMono-Regular, Menlo, monospace; }
.diff div { padding:0 12px; white-space:pre-wrap; word-break:break-all; }
.op-add { background:rgba(103,194,58,.16); color:var(--el-color-success-dark-2); }
.op-del { background:rgba(245,108,108,.16); color:var(--el-color-danger-dark-2); }
.op-skip { color:var(--el-text-color-placeholder); font-style:italic; }
</style>
