<template>
  <router-view />
</template>

<style>
body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif; }
.log-pre { background:#11161d; color:#c9d1d9; padding:10px; border-radius:6px; max-height:360px; overflow:auto;
           font-size:12px; line-height:1.5; white-space:pre-wrap; word-break:break-all; margin:0; }
.log-dialog { width: min(720px, 92vw) !important; max-width: none !important; }
.muted { color: var(--el-text-color-secondary); font-size: 12px; }
</style>
