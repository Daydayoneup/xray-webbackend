import { h } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { xrayApi } from '../api/index.js'
import { apiError, apiLogs } from '../api/http.js'

function logBox(title, msg, logs) {
  return ElMessageBox.alert(
    h('div', [
      h('p', { style: 'margin:0 0 8px' }, msg),
      logs.length ? h('pre', { class: 'log-pre' }, logs.join('\n')) : null,
    ]),
    title, { type: 'error', confirmButtonText: '知道了', customClass: 'log-dialog' },
  ).catch(() => {})
}

// 展示应用/切换失败的原因与 Xray 日志
export function showApplyError(e, title = '操作失败') {
  return logBox(title, apiError(e), apiLogs(e))
}

// 应用配置：成功提示（含警告），失败弹窗展示原因与 Xray 日志
export async function applyConfig(panel) {
  try {
    const { data } = await xrayApi.apply()
    await panel.refreshAll()
    if (data.warnings?.length) {
      ElMessage({ type: 'warning', duration: 6000, message: `已应用，但有 ${data.warnings.length} 条提示：${data.warnings[0]}` })
    } else {
      ElMessage.success('已应用，Xray 已重启')
    }
    return true
  } catch (e) {
    await panel.refreshStatus().catch(() => {})
    await logBox('应用失败', apiError(e), apiLogs(e))
    return false
  }
}

export async function restartXray(panel) {
  try {
    await xrayApi.restart(); await panel.refreshStatus(); ElMessage.success('已重启')
  } catch (e) {
    await panel.refreshStatus().catch(() => {})
    await logBox('重启失败', apiError(e), apiLogs(e))
  }
}
