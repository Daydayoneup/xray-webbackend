<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { authApi, xrayApi, opsApi } from '../api/index.js'
import { apiError, setToken } from '../api/http.js'
import { fmtTime, fmtBytes } from '../utils/format.js'
import { usePanel } from '../stores/panel.js'
import { showApplyError } from '../utils/apply.js'
import DiffView from '../components/DiffView.vue'

const panel = usePanel()
const route = useRoute()
const router = useRouter()
const tab = ref(route.query.tab || 'account')
watch(tab, (t) => router.replace({ query: { tab: t } }))

const S = ref(null)          // 设置原始数据
const busy = reactive({})
async function loadSettings() { try { S.value = (await opsApi.settings()).data; fillForms() } catch (e) { ElMessage.error(apiError(e)) } }
onMounted(() => { loadSettings(); loadConfig(); loadHistory() })

const lines = (s) => s.split('\n').map((x) => x.trim()).filter(Boolean)
async function saveSettings(body, msg = '已保存') {
  try { S.value = (await opsApi.saveSettings(body)).data; await loadSettings(); await panel.refreshStatus(); ElMessage.success(msg); return true }
  catch (e) { ElMessage.error(apiError(e)); return false }
}

// ---- 账户与服务器 ----
const pw = reactive({ old_password: '', new_password: '', confirm: '' })
const host = ref('')
async function changePw() {
  if (pw.new_password.length < 6) return ElMessage.warning('新密码至少 6 位')
  if (pw.new_password !== pw.confirm) return ElMessage.warning('两次输入的新密码不一致')
  try {
    const { data } = await authApi.changePassword(pw.old_password, pw.new_password)
    if (data.token) setToken(data.token)
    Object.assign(pw, { old_password: '', new_password: '', confirm: '' })
    ElMessage.success('密码已修改，其他设备需要重新登录')
    panel.refreshStatus().catch(() => {})
  } catch (e) { ElMessage.error(apiError(e)) }
}

// ---- DNS ----
const DNS_MODES = [
  ['system', '使用系统 DNS', '不做额外设置（默认）。服务器在海外时通常够用'],
  ['split', '国内外分流', '国内域名用国内 DNS 直连查询，其余用加密 DNS 经代理查询。服务器在国内时推荐，可避免 DNS 污染'],
  ['doh', '全部加密 DNS', '所有查询都用 DoH（经默认出口），防止 DNS 泄漏'],
  ['custom', '自定义', '手动填写 Xray DNS 服务器列表'],
]
const dns = reactive({ mode: 'system', domestic: '', remote: '', custom: '', query_strategy: 'UseIP' })

// ---- 通知 ----
const NOTIFY_EMPTY = { telegram_token: '', telegram_chat_id: '', bark_url: '', feishu_url: '', feishu_secret: '',
  dingtalk_url: '', dingtalk_secret: '', wecom_url: '', webhook_url: '', events: [] }
const notify = reactive({ ...NOTIFY_EMPTY })
const CHANNELS = [
  { key: 'feishu', name: '飞书', fields: [['feishu_url', 'https://open.feishu.cn/open-apis/bot/v2/hook/…'], ['feishu_secret', '签名密钥（可选）', true]],
    help: '群设置 → 群机器人 → 添加「自定义机器人」，复制 Webhook 地址；若开启了「签名校验」，同时填写密钥' },
  { key: 'dingtalk', name: '钉钉', fields: [['dingtalk_url', 'https://oapi.dingtalk.com/robot/send?access_token=…'], ['dingtalk_secret', '加签密钥（可选）', true]],
    help: '群设置 → 机器人 → 添加「自定义」机器人。安全设置选「加签」时填写密钥；选「自定义关键词」时请包含关键词「Xray」' },
  { key: 'wecom', name: '企业微信', fields: [['wecom_url', 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…']],
    help: '群聊 → 右上角 … → 添加群机器人 → 新创建一个机器人，复制 Webhook 地址' },
  { key: 'telegram', name: 'Telegram', fields: [['telegram_token', 'Bot Token', true], ['telegram_chat_id', 'Chat ID']],
    help: '找 @BotFather 创建机器人获取 Token；服务器无法直连时会自动经本机 HTTP 入站代理发送' },
  { key: 'bark', name: 'Bark', fields: [['bark_url', 'https://api.day.app/你的Key']], help: 'iOS 推送 App，打开 App 复制服务器地址' },
  { key: 'webhook', name: '通用 Webhook', fields: [['webhook_url', 'https://…']],
    help: '以 POST 发送 JSON：{ "title": "...", "body": "...", "time": 1700000000 }，可对接自建服务' },
]
const configured = (c) => c.fields.some(([k, , secret]) => !secret && notify[k])

// ---- 规则数据 ----
const geo = reactive({ source: 'loyalsoldier', auto_update_days: 0 })

function fillForms() {
  const s = S.value
  host.value = s.public_host || ''
  Object.assign(dns, { mode: s.dns.mode || 'system', domestic: (s.dns.domestic || []).join('\n'), remote: (s.dns.remote || []).join('\n'),
    custom: (s.dns.custom || []).join('\n'), query_strategy: s.dns.query_strategy || 'UseIP' })
  Object.assign(notify, { ...NOTIFY_EMPTY, ...s.notify, events: s.notify.events || [] })
  Object.assign(geo, { source: s.geo.source || 'loyalsoldier', auto_update_days: s.geo.auto_update_days || 0 })
}
const saveDNS = () => saveSettings({ dns: { mode: dns.mode, domestic: lines(dns.domestic), remote: lines(dns.remote),
  custom: lines(dns.custom), query_strategy: dns.query_strategy } }, '已保存，应用后生效')
const saveNotify = () => saveSettings({ notify: { ...notify } })
async function testNotify() {
  if (!(await saveNotify())) return
  busy.notify = true
  try { await opsApi.testNotify(); ElMessage.success('测试消息已发送，请检查是否收到') }
  catch (e) { ElMessage.error(apiError(e)) }
  finally { busy.notify = false }
}
const saveGeo = () => saveSettings({ geo: { ...geo } })
async function updateGeo() {
  if (!(await saveGeo())) return
  busy.geo = true
  try { await opsApi.updateGeo(); await loadSettings(); ElMessage.success('规则数据库已更新') }
  catch (e) { await loadSettings(); ElMessage.error(apiError(e)) }
  finally { busy.geo = false }
}

// ---- 备份与历史 ----
const history = ref([])
const diffDlg = ref(false)
const diffLines = ref([])
async function loadHistory() { try { history.value = (await opsApi.history()).data } catch (_) { history.value = [] } }
async function exportBackup() {
  try {
    const { data } = await opsApi.exportBackup()
    const a = document.createElement('a')
    a.href = URL.createObjectURL(data)
    a.download = `xray-panel-backup-${new Date().toISOString().slice(0, 10)}.json`
    a.click()
  } catch (e) { ElMessage.error(apiError(e)) }
}
function importBackup(ev) {
  const f = ev.target.files[0]; ev.target.value = ''
  if (!f) return
  const rd = new FileReader()
  rd.onload = async () => {
    try {
      await ElMessageBox.confirm('导入会替换当前全部面板数据（登录密码不变）。导入前会自动保存一份历史快照，可以回滚。继续？', '导入备份', { type: 'warning' })
    } catch (_) { return }
    try {
      const { data } = await opsApi.importBackup(rd.result)
      ElMessage.success(`已导入：${data.summary}。点击右上角「应用并重启」生效`)
      await loadSettings(); await loadHistory(); await panel.refreshAll()
    } catch (e) { ElMessage.error(apiError(e)) }
  }
  rd.readAsText(f)
}
async function showDiff(h) {
  try { diffLines.value = (await opsApi.historyDiff(h.id)).data.lines; diffDlg.value = true }
  catch (e) { ElMessage.error(apiError(e)) }
}
async function restore(h) {
  try {
    await ElMessageBox.confirm(`恢复到 ${fmtTime(h.time)} 的配置并立即应用？当前数据会先自动保存一份快照。`, '回滚配置', { type: 'warning' })
  } catch (_) { return }
  busy['r' + h.id] = true
  try { await opsApi.restore(h.id); ElMessage.success('已回滚并应用'); await loadHistory(); await panel.refreshAll() }
  catch (e) { await showApplyError(e, '回滚失败') }
  finally { busy['r' + h.id] = false }
}

// ---- 配置查看 ----
const cfgTab = ref('applied')
const rawConfig = ref('')
const warnings = ref([])
async function loadConfig() {
  warnings.value = []
  if (cfgTab.value === 'applied') {
    try { rawConfig.value = JSON.stringify((await xrayApi.config()).data, null, 2) } catch (_) { rawConfig.value = '(尚未应用过配置)' }
  } else {
    try { const { data } = await xrayApi.draft(); rawConfig.value = JSON.stringify(data.config, null, 2); warnings.value = data.warnings }
    catch (e) { rawConfig.value = apiError(e) }
  }
}
watch(cfgTab, loadConfig)
async function copy() {
  try { await navigator.clipboard.writeText(rawConfig.value); ElMessage.success('已复制') } catch (_) { ElMessage.warning('复制失败，请手动复制') }
}
</script>

<template>
  <el-card>
    <el-tabs v-model="tab">
      <el-tab-pane label="账户与服务器" name="account">
        <h4>修改密码</h4>
        <el-form label-width="100px" style="max-width:440px">
          <el-form-item label="原密码"><el-input v-model="pw.old_password" type="password" show-password /></el-form-item>
          <el-form-item label="新密码"><el-input v-model="pw.new_password" type="password" show-password placeholder="至少 6 位" /></el-form-item>
          <el-form-item label="确认新密码"><el-input v-model="pw.confirm" type="password" show-password @keyup.enter="changePw" /></el-form-item>
          <el-form-item><el-button type="primary" @click="changePw">修改密码</el-button></el-form-item>
          <el-form-item>
            <span class="muted">忘记密码时在服务器上执行 <code>xray-panel passwd</code>（Docker：<code>docker exec xray-panel /xray-panel passwd</code>）重置。</span>
          </el-form-item>
        </el-form>
        <h4>服务器地址</h4>
        <el-form label-width="100px" style="max-width:560px">
          <el-form-item label="公网地址">
            <el-input v-model="host" placeholder="客户端连接用的 IP 或域名，如 1.2.3.4" />
            <div class="muted">用于生成入站的分享链接和二维码</div>
          </el-form-item>
          <el-form-item><el-button type="primary" @click="saveSettings({ public_host: host })">保存</el-button></el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="DNS" name="dns">
        <el-radio-group v-model="dns.mode" class="cards">
          <el-radio v-for="[v, l, d] in DNS_MODES" :key="v" :value="v" border><b>{{ l }}</b><div class="muted">{{ d }}</div></el-radio>
        </el-radio-group>
        <el-form label-width="110px" style="max-width:640px;margin-top:16px">
          <el-form-item v-if="dns.mode === 'split'" label="国内 DNS">
            <el-input v-model="dns.domestic" type="textarea" :rows="2" :placeholder="S?.dns_defaults.domestic.join('\n')" />
            <div class="muted">每行一个；留空使用默认值。用于 geosite:cn 域名，直连查询</div>
          </el-form-item>
          <el-form-item v-if="dns.mode === 'split' || dns.mode === 'doh'" label="加密 DNS">
            <el-input v-model="dns.remote" type="textarea" :rows="2" :placeholder="S?.dns_defaults.remote.join('\n')" />
            <div class="muted">每行一个；留空使用默认值。经默认出口查询</div>
          </el-form-item>
          <el-form-item v-if="dns.mode === 'custom'" label="DNS 服务器">
            <el-input v-model="dns.custom" type="textarea" :rows="4" placeholder="每行一个，如 https://1.1.1.1/dns-query 或 8.8.8.8" />
          </el-form-item>
          <el-form-item v-if="dns.mode !== 'system'" label="IP 版本">
            <el-select v-model="dns.query_strategy" style="width:220px">
              <el-option label="IPv4 + IPv6" value="UseIP" /><el-option label="仅 IPv4" value="UseIPv4" /><el-option label="仅 IPv6" value="UseIPv6" />
            </el-select>
          </el-form-item>
          <el-form-item><el-button type="primary" @click="saveDNS">保存</el-button></el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="通知" name="notify">
        <p class="muted" style="margin-top:0">配置任意一个或多个渠道，提醒会同时发到所有已配置的渠道。直连失败时会自动经本机 HTTP 入站代理重试。</p>
        <el-form label-width="120px" style="max-width:720px">
          <el-form-item v-for="c in CHANNELS" :key="c.key">
            <template #label>
              <span>{{ c.name }}</span><el-tag v-if="configured(c)" size="small" type="success" style="margin-left:4px">已配置</el-tag>
            </template>
            <div class="row" style="width:100%">
              <el-input v-for="[k, ph, secret] in c.fields" :key="k" v-model="notify[k]" :placeholder="ph" clearable
                        :type="secret ? 'password' : 'text'" :show-password="!!secret" :class="secret ? 'secret' : 'grow'" />
            </div>
            <div class="muted" style="width:100%">{{ c.help }}</div>
          </el-form-item>
          <el-form-item label="提醒事件">
            <el-checkbox-group v-model="notify.events">
              <el-checkbox v-for="e in S?.notify_events || []" :key="e.id" :value="e.id">{{ e.label }}</el-checkbox>
            </el-checkbox-group>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" @click="saveNotify">保存</el-button>
            <el-button :loading="busy.notify" @click="testNotify">保存并发送测试消息</el-button>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="规则数据库" name="geo">
        <p class="muted" style="margin-top:0">geoip / geosite 是「地区IP」「预置域名集合」规则使用的数据，定期更新可让分流更准确。</p>
        <el-descriptions v-if="S" :column="1" border style="max-width:640px">
          <el-descriptions-item label="当前使用">{{ S.geo_files.custom ? '面板下载的数据' : '镜像自带的数据' }}（{{ S.geo_files.dir }}）</el-descriptions-item>
          <el-descriptions-item label="文件大小">geoip {{ fmtBytes(S.geo_files.geoip_size) }} · geosite {{ fmtBytes(S.geo_files.geosite_size) }}</el-descriptions-item>
          <el-descriptions-item label="上次更新">{{ S.geo.updated_at ? fmtTime(S.geo.updated_at) : (S.geo_files.mod_time ? `文件时间 ${fmtTime(S.geo_files.mod_time)}` : '—') }}</el-descriptions-item>
          <el-descriptions-item v-if="S.geo.last_error" label="上次失败"><span class="err">{{ S.geo.last_error }}</span></el-descriptions-item>
        </el-descriptions>
        <el-form label-width="100px" style="max-width:640px;margin-top:16px">
          <el-form-item label="数据来源">
            <el-select v-model="geo.source" style="width:260px">
              <el-option v-for="s in S?.geo_sources || []" :key="s.id" :label="s.name" :value="s.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="自动更新">
            <el-select v-model="geo.auto_update_days" style="width:260px">
              <el-option label="关闭" :value="0" /><el-option label="每天" :value="1" /><el-option label="每周" :value="7" /><el-option label="每月" :value="30" />
            </el-select>
          </el-form-item>
          <el-form-item>
            <el-button @click="saveGeo">保存</el-button>
            <el-button type="primary" :loading="busy.geo" @click="updateGeo">{{ busy.geo ? '下载中…' : '立即更新' }}</el-button>
            <span class="muted" style="margin-left:8px">更新成功后会自动重启 Xray 使其生效</span>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="备份与历史" name="backup">
        <h4 style="margin-top:0">备份与恢复</h4>
        <p class="muted">导出全部订阅、节点、规则、入站、设置（不含登录密码），可用于迁移到新服务器。</p>
        <el-button type="primary" @click="exportBackup">导出备份</el-button>
        <el-button @click="$refs.imp.click()">导入备份</el-button>
        <input ref="imp" type="file" accept="application/json" hidden @change="importBackup" />

        <h4>配置历史（最近 10 次应用）</h4>
        <el-table :data="history" size="small" empty-text="还没有应用记录">
          <el-table-column label="时间" width="170"><template #default="{ row }">{{ fmtTime(row.time) }}</template></el-table-column>
          <el-table-column label="类型" width="140"><template #default="{ row }">{{ row.note }}</template></el-table-column>
          <el-table-column label="内容" min-width="240"><template #default="{ row }">
            {{ row.summary }}<el-tag v-if="row.warnings?.length" size="small" type="warning" style="margin-left:6px">{{ row.warnings.length }} 条提示</el-tag>
          </template></el-table-column>
          <el-table-column label="操作" width="170"><template #default="{ row }">
            <el-button size="small" @click="showDiff(row)">与当前对比</el-button>
            <el-button size="small" type="warning" :loading="busy['r' + row.id]" @click="restore(row)">回滚</el-button>
          </template></el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="配置查看" name="config">
        <div class="row" style="justify-content:space-between;margin-bottom:8px">
          <el-radio-group v-model="cfgTab" size="small">
            <el-radio-button value="applied">当前生效的 config.json</el-radio-button>
            <el-radio-button value="draft">待应用的配置（预览）</el-radio-button>
          </el-radio-group>
          <el-button size="small" @click="copy">复制</el-button>
        </div>
        <el-alert v-for="(w, i) in warnings" :key="i" :title="w" type="warning" :closable="false" style="margin-bottom:6px" />
        <el-input v-model="rawConfig" type="textarea" :rows="22" readonly class="mono" />
      </el-tab-pane>
    </el-tabs>
  </el-card>

  <el-dialog v-model="diffDlg" title="与当前生效配置的差异（左：历史 → 右：当前）" width="min(860px, 96vw)">
    <DiffView :lines="diffLines" empty="与当前生效配置相同" />
  </el-dialog>
</template>

<style scoped>
h4 { margin:18px 0 10px; }
.row { display:flex; gap:8px; align-items:center; flex-wrap:wrap; }
.grow { flex:1; min-width:200px; }
.err { color:var(--el-color-danger); }
.secret { width:240px; }
.cards { display:grid; grid-template-columns:1fr 1fr; gap:8px; max-width:760px; }
.cards :deep(.el-radio) { height:auto; padding:8px 12px; margin-right:0; align-items:flex-start; }
.cards :deep(.el-radio__label) { white-space:normal; }
.mono :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size:12px; }
@media (max-width:600px) { .cards { grid-template-columns:1fr; } }
</style>
