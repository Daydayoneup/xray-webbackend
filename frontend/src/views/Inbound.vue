<script setup>
import { ref, onMounted, reactive, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import QRCode from 'qrcode'
import { inboundApi, opsApi, userApi } from '../api/index.js'
import { apiError } from '../api/http.js'
import { usePanel } from '../stores/panel.js'

const emit = defineEmits(['changed'])
const list = ref([])
const users = ref([])
const panel = usePanel()
const advanced = ref([])
const dialog = ref(false)
const saving = ref(false)
const editing = ref(null)

const PROTOCOLS = [
  { v: 'socks', name: 'SOCKS5', desc: '最通用，适合本机或局域网使用；明文传输' },
  { v: 'http', name: 'HTTP', desc: '浏览器/系统代理设置可直接使用；明文传输' },
  { v: 'vless', name: 'VLESS + Reality', desc: '加密且难以识别，手机/外网连接推荐；无需证书' },
  { v: 'shadowsocks', name: 'Shadowsocks 2022', desc: '加密，客户端支持广泛' },
]
const protoName = (v) => PROTOCOLS.find((p) => p.v === v)?.name || v
const SS_METHODS = ['2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305']

const EMPTY = { protocol: 'socks', remark: '', listen: '127.0.0.1', port: null, udp: true, users: [],
  reality_dest: 'www.microsoft.com:443', reality_server_names: '', reality_private_key: '', reality_public_key: '', reality_short_ids: '',
  ss_method: '2022-blake3-aes-128-gcm', ss_key: '', dns_server: '', dns_via: '' }
const form = reactive({ ...EMPTY })
const encrypted = computed(() => form.protocol === 'vless' || form.protocol === 'shadowsocks')
// DNS 出口：直连、订阅节点、自定义代理（Xray 限制：节点组不能用作 dialerProxy）
const dnsVias = computed(() => panel.outbounds.filter((o) => o.kind === 'node' || o.kind === 'proxy'))
const DNS_PRESETS = [
  { v: '223.5.5.5', l: '阿里 223.5.5.5' }, { v: '119.29.29.29', l: '腾讯 119.29.29.29' },
  { v: '8.8.8.8', l: 'Google 8.8.8.8' }, { v: '1.1.1.1', l: 'Cloudflare 1.1.1.1' },
]
const userLabel = (n) => { const u = users.value.find((x) => x.name === n); return u?.disabled ? `${n}（已停用）` : n }

async function load() {
  const [a, b] = await Promise.all([inboundApi.list(), userApi.list().catch(() => ({ data: [] }))])
  list.value = a.data; users.value = b.data
}
onMounted(() => { load(); if (!panel.outbounds.length) panel.refreshOutbounds().catch(() => {}) })

function openCreate() {
  editing.value = null
  Object.assign(form, structuredClone(EMPTY))
  advanced.value = []
  dialog.value = true
}
function openEdit(row) {
  editing.value = row.tag
  const r = row.reality || {}
  Object.assign(form, structuredClone(EMPTY), {
    protocol: row.protocol, remark: row.remark || '', listen: row.listen, port: row.port, udp: row.udp ?? true,
    users: [...(row.users || [])], dns_server: row.dns?.server || '', dns_via: row.dns?.via || '',
    reality_dest: r.dest || EMPTY.reality_dest, reality_server_names: (r.server_names || []).join(', '),
    reality_private_key: r.private_key || '', reality_public_key: r.public_key || '', reality_short_ids: (r.short_ids || []).join(', '),
    ss_method: row.ss_method || EMPTY.ss_method, ss_key: row.ss_key || '',
  })
  advanced.value = row.dns?.server ? ['adv'] : []
  dialog.value = true
}
function onProtocol(p) {
  form.users = []
  if (p === 'vless' || p === 'shadowsocks') {
    if (form.listen === '127.0.0.1') form.listen = '0.0.0.0'
    form.users = users.value.filter((u) => !u.disabled).map((u) => u.name) // 加密入站默认关联全部启用的用户
  }
}

async function genSSKey() {
  try { form.ss_key = (await inboundApi.gen('sskey', form.ss_method)).data.value }
  catch (e) { ElMessage.error(apiError(e)) }
}
async function genReality() {
  try {
    const { data } = await inboundApi.gen('reality')
    Object.assign(form, { reality_private_key: data.private_key, reality_public_key: data.public_key, reality_short_ids: data.short_id })
  } catch (e) { ElMessage.error(apiError(e)) }
}
const split = (s) => s.split(/[,\s]+/).map((x) => x.trim()).filter(Boolean)

function payload() {
  const p = { protocol: form.protocol, remark: form.remark, listen: form.listen.trim() || '127.0.0.1', port: form.port, udp: form.udp,
    users: form.users, dns: { server: form.dns_server.trim(), via: form.dns_server.trim() ? form.dns_via : '' } }
  if (form.protocol === 'vless') Object.assign(p, { reality_dest: form.reality_dest, reality_server_names: split(form.reality_server_names),
    reality_private_key: form.reality_private_key, reality_short_ids: split(form.reality_short_ids) })
  if (form.protocol === 'shadowsocks') Object.assign(p, { ss_method: form.ss_method, ss_key: form.ss_key })
  return p
}
async function save() {
  if (!form.port) return ElMessage.warning('请填写端口')
  saving.value = true
  try {
    if (editing.value) await inboundApi.update(editing.value, payload())
    else await inboundApi.create(payload())
    dialog.value = false; await load(); emit('changed'); ElMessage.success('已保存，应用后生效')
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { saving.value = false }
}
async function remove(row) {
  try { await ElMessageBox.confirm(`删除入站 ${row.remark || protoName(row.protocol)}:${row.port}？`, '确认', { type: 'warning' }) } catch (_) { return }
  try { await inboundApi.remove(row.tag); await load(); emit('changed') }
  catch (e) { ElMessage.error(apiError(e)) }
}
function risky(row) {
  return ['0.0.0.0', '::'].includes(row.listen) && ['socks', 'http'].includes(row.protocol) && !row.active_users
}

// ---- 分享 ----
const shareDlg = ref(false)
const shareRow = ref(null)
const shareHost = ref('')
const shareItems = ref([])
const qr = reactive({})
async function openShare(row) {
  shareRow.value = row
  if (!shareHost.value) {
    try { shareHost.value = (await opsApi.settings()).data.public_host || window.location.hostname } catch (_) { shareHost.value = window.location.hostname }
  }
  shareDlg.value = true
  await loadShare()
}
async function loadShare() {
  try {
    const { data } = await inboundApi.share(shareRow.value.tag, shareHost.value.trim())
    shareItems.value = data.items
    for (const it of data.items) qr[it.link] = await QRCode.toDataURL(it.link, { margin: 1, width: 180 })
  } catch (e) { shareItems.value = []; ElMessage.error(apiError(e)) }
}
async function rememberHost() {
  try { await opsApi.saveSettings({ public_host: shareHost.value.trim() }); ElMessage.success('已保存为默认服务器地址') }
  catch (e) { ElMessage.error(apiError(e)) }
}
async function copy(text) {
  try { await navigator.clipboard.writeText(text); ElMessage.success('已复制') } catch (_) { ElMessage.warning('复制失败，请手动复制') }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>入站（设备连接本服务器用的代理端口）</span>
        <el-button type="primary" @click="openCreate">+ 新建入站</el-button></div>
    </template>
    <el-table :data="list">
      <el-table-column label="名称 / 协议" min-width="160"><template #default="{ row }">
        <div>{{ row.remark || protoName(row.protocol) }}</div>
        <div v-if="row.remark" class="muted">{{ protoName(row.protocol) }}</div>
      </template></el-table-column>
      <el-table-column label="监听" min-width="140"><template #default="{ row }">
        <div>{{ row.listen }}:{{ row.port }}</div>
        <div v-if="row.dns" class="muted">DNS {{ row.dns.server }}{{ row.dns.via ? ' 经 ' + panel.label(row.dns.via) : '' }}</div>
      </template></el-table-column>
      <el-table-column label="用户" min-width="180"><template #default="{ row }">
        <el-tag v-if="risky(row)" type="warning" size="small">⚠ 对外开放且无密码</el-tag>
        <template v-else-if="row.users.length">
          <el-tag v-for="n in row.users.slice(0, 4)" :key="n" size="small" class="utag" :type="users.find(u => u.name === n)?.disabled ? 'info' : 'primary'" effect="plain">{{ n }}</el-tag>
          <span v-if="row.users.length > 4" class="muted">+{{ row.users.length - 4 }}</span>
        </template>
        <span v-else-if="['vless', 'shadowsocks'].includes(row.protocol)" class="warn-text">未关联用户，不会生效</span>
        <span v-else class="muted">无需认证</span>
      </template></el-table-column>
      <el-table-column label="操作" width="200"><template #default="{ row }">
        <el-button size="small" @click="openShare(row)">分享</el-button>
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑入站' : '新建入站'" width="min(680px, 96vw)">
    <el-form label-width="96px">
      <el-form-item label="协议">
        <el-radio-group v-model="form.protocol" class="proto" :disabled="!!editing" @change="onProtocol">
          <el-radio v-for="p in PROTOCOLS" :key="p.v" :value="p.v" border>
            <b>{{ p.name }}</b><div class="muted">{{ p.desc }}</div>
          </el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="名称"><el-input v-model="form.remark" placeholder="可选，如 手机用" maxlength="40" /></el-form-item>
      <el-form-item label="监听地址">
        <el-select v-model="form.listen" filterable allow-create default-first-option style="width:100%">
          <el-option label="127.0.0.1（仅本机可用）" value="127.0.0.1" />
          <el-option label="0.0.0.0（局域网/外网可连接）" value="0.0.0.0" />
          <el-option label="::（IPv4 + IPv6 全部网卡）" value="::" />
        </el-select>
      </el-form-item>
      <el-form-item label="端口"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>
      <el-form-item v-if="form.protocol === 'socks'" label="UDP"><el-switch v-model="form.udp" /></el-form-item>

      <template v-if="form.protocol === 'vless'">
        <el-form-item label="伪装目标">
          <el-input v-model="form.reality_dest" placeholder="www.microsoft.com:443" />
          <div class="muted" style="width:100%">一个境外可访问、支持 TLS 1.3 的大网站，外界看到的是访问它的流量</div>
        </el-form-item>
        <el-form-item label="SNI"><el-input v-model="form.reality_server_names" placeholder="默认与伪装目标的域名相同" /></el-form-item>
        <el-form-item label="密钥">
          <div class="row" style="width:100%">
            <el-input v-model="form.reality_private_key" placeholder="留空自动生成" class="grow" />
            <el-button @click="genReality">重新生成</el-button>
          </div>
          <div v-if="form.reality_public_key" class="muted" style="width:100%">公钥：{{ form.reality_public_key }}</div>
        </el-form-item>
        <el-form-item label="ShortId"><el-input v-model="form.reality_short_ids" placeholder="留空自动生成" /></el-form-item>
      </template>
      <template v-if="form.protocol === 'shadowsocks'">
        <el-form-item label="加密方式">
          <el-select v-model="form.ss_method" @change="form.ss_key = ''">
            <el-option v-for="m in SS_METHODS" :key="m" :label="m" :value="m" />
          </el-select>
        </el-form-item>
        <el-form-item label="服务端密钥">
          <div class="row" style="width:100%">
            <el-input v-model="form.ss_key" placeholder="留空自动生成" class="grow" />
            <el-button @click="genSSKey">生成</el-button>
          </div>
        </el-form-item>
      </template>

      <el-form-item label="用户">
        <div style="width:100%">
          <el-select v-model="form.users" multiple filterable style="width:100%"
                     :placeholder="encrypted ? '选择可以连接的用户' : '不选择则无需认证'">
            <el-option v-for="u in users" :key="u.name" :value="u.name" :label="userLabel(u.name)">
              <span>{{ u.name }}</span><span v-if="u.remark" class="muted"> · {{ u.remark }}</span>
              <el-tag v-if="u.disabled" size="small" type="info" style="margin-left:6px">已停用</el-tag>
            </el-option>
          </el-select>
          <div class="muted">
            {{ encrypted ? '至少关联一个用户，否则该入站不会生效。' : '关联用户后使用「用户名 + 密码」认证，对外开放时建议设置。' }}
            用户及其密码/UUID 在 <el-link type="primary" :underline="false" @click="dialog = false; $router.push('/users')">用户</el-link> 页管理。
          </div>
        </div>
      </el-form-item>

      <el-collapse v-model="advanced" class="adv">
        <el-collapse-item name="adv" title="高级设置">
          <el-form-item label="独立 DNS">
            <div style="width:100%">
              <el-select v-model="form.dns_server" filterable allow-create default-first-option clearable
                         placeholder="不设置（使用客户端请求的 DNS 服务器）" style="width:100%">
                <el-option v-for="p in DNS_PRESETS" :key="p.v" :label="p.l" :value="p.v" />
              </el-select>
              <div class="muted">设置后，经此入站发出的 DNS 查询（目标端口 53）都改发到这个服务器，每个入站可以不同。
                可填 IP 或 IP:端口。面板「设置 → DNS」是 Xray 自身解析域名（分流判断、直连）用的，对所有入站生效。</div>
            </div>
          </el-form-item>
          <el-form-item v-if="form.dns_server" label="DNS 出口">
            <el-select v-model="form.dns_via" filterable style="width:100%">
              <el-option label="直连" value="" />
              <el-option v-for="o in dnsVias" :key="o.tag" :value="o.tag" :label="o.label" :disabled="!o.available">
                <span>{{ o.label }}</span><span class="muted"> · {{ o.kind === 'node' ? '订阅节点' : '自定义代理' }}{{ o.available ? '' : '（不可用）' }}</span>
              </el-option>
            </el-select>
            <div class="muted">国内 DNS 选直连；境外 DNS（如 8.8.8.8）建议经某个节点访问，避免被污染。</div>
          </el-form-item>
        </el-collapse-item>
      </el-collapse>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
  </el-dialog>

  <el-dialog v-model="shareDlg" title="分享给客户端" width="min(720px, 96vw)">
    <div class="row" style="margin-bottom:12px">
      <span>服务器地址</span>
      <el-input v-model="shareHost" placeholder="客户端连接用的公网 IP 或域名" class="grow" @keyup.enter="loadShare" />
      <el-button @click="loadShare">生成</el-button>
      <el-button text @click="rememberHost">设为默认</el-button>
    </div>
    <el-alert v-if="shareRow && ['127.0.0.1', '::1'].includes(shareRow.listen)" type="warning" :closable="false" show-icon
              title="该入站只监听本机，其他设备无法连接；如需外部使用，请把监听地址改为 0.0.0.0" style="margin-bottom:12px" />
    <div v-for="it in shareItems" :key="it.link" class="share" :class="{ off: it.disabled }">
      <img v-if="qr[it.link]" :src="qr[it.link]" alt="二维码" class="qr" />
      <div class="grow">
        <b>{{ it.name }}</b><el-tag v-if="it.disabled" size="small" type="info" style="margin-left:6px">已停用</el-tag>
        <div class="link">{{ it.link }}</div>
        <el-button size="small" @click="copy(it.link)">复制链接</el-button>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped>
.hd { display:flex; justify-content:space-between; align-items:center; }
.row { display:flex; gap:8px; align-items:center; }
.grow { flex:1; min-width:120px; }
.proto { display:grid; grid-template-columns:1fr 1fr; gap:8px; width:100%; }
.proto :deep(.el-radio) { height:auto; padding:8px 12px; margin-right:0; align-items:flex-start; }
.proto :deep(.el-radio__label) { white-space:normal; }
.utag { margin:0 4px 2px 0; }
.warn-text { color:var(--el-color-warning); font-size:12px; }
.adv { border-top:none; margin-top:4px; }
.adv :deep(.el-collapse-item__header) { font-size:13px; color:var(--el-text-color-secondary); padding-left:96px; }
.share { display:flex; gap:14px; padding:10px 0; border-bottom:1px solid var(--el-border-color-lighter); }
.share.off { opacity:.5; }
.qr { width:140px; height:140px; border-radius:6px; background:#fff; }
.link { font:12px/1.5 ui-monospace, Menlo, monospace; word-break:break-all; color:var(--el-text-color-secondary); margin:6px 0; }
@media (max-width:600px) { .proto { grid-template-columns:1fr; } }
</style>
