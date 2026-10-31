<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import QRCode from 'qrcode'
import { userApi, inboundApi, opsApi } from '../api/index.js'
import { apiError } from '../api/http.js'
import { fmtBytes } from '../utils/format.js'

const emit = defineEmits(['changed'])
const list = ref([])
const inbounds = ref([])
const loading = ref(false)
let timer = null

const PROTO = { socks: 'SOCKS5', http: 'HTTP', vless: 'VLESS', shadowsocks: 'SS2022' }
const ibLabel = (ib) => `${ib.remark || PROTO[ib.protocol] || ib.protocol} :${ib.port}`
// 入站有名称时再附上协议，避免出现「SOCKS5 :18808 SOCKS5」
const ibMap = computed(() => Object.fromEntries(inbounds.value.map((i) => [i.tag, i])))

async function load() {
  loading.value = !list.value.length
  try {
    const [u, i] = await Promise.all([userApi.list(), inboundApi.list()])
    list.value = u.data; inbounds.value = i.data
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { loading.value = false }
}
onMounted(() => { load(); timer = setInterval(load, 15000) })
onBeforeUnmount(() => clearInterval(timer))

const onlineCount = computed(() => list.value.filter((u) => u.online).length)

// ---- 编辑 ----
const dialog = ref(false)
const editing = ref(null)
const saving = ref(false)
const showCred = ref(false)
// 凭据与协议的对应：只突出显示所选入站用得到的凭据
const CREDS = [
  { key: 'uuid', label: 'UUID', proto: ' VLESS', protos: ['vless'], gen: () => genUUID() },
  { key: 'password', label: '密码', proto: ' SOCKS / HTTP', protos: ['socks', 'http'], gen: () => genPassword() },
  { key: 'ss_key', label: 'SS 密钥', proto: ' Shadowsocks', protos: ['shadowsocks'], gen: () => genSS() },
]
const credInfo = computed(() => CREDS.map((c) => ({
  ...c,
  usedBy: form.inbounds.map((t) => ibMap.value[t]).filter((ib) => ib && c.protos.includes(ib.protocol)).map(ibLabel),
})))
const otherCreds = computed(() => credInfo.value.filter((c) => !c.usedBy.length))
const shownCreds = computed(() => credInfo.value.filter((c) => c.usedBy.length || showCred.value))
const EMPTY = { name: '', remark: '', disabled: false, uuid: '', password: '', ss_key: '', inbounds: [] }
const form = reactive({ ...EMPTY })

function openCreate() {
  editing.value = null
  Object.assign(form, structuredClone(EMPTY))
  showCred.value = false
  dialog.value = true
}
function openEdit(row) {
  editing.value = row.name
  Object.assign(form, structuredClone(EMPTY), {
    name: row.name, remark: row.remark || '', disabled: !!row.disabled,
    uuid: row.uuid, password: row.password, ss_key: row.ss_key, inbounds: [...row.inbounds],
  })
  showCred.value = false
  dialog.value = true
}
const body = (f) => ({ name: f.name.trim(), remark: f.remark, disabled: f.disabled, uuid: f.uuid,
  password: f.password, ss_key: f.ss_key, inbounds: f.inbounds })

async function save() {
  if (!form.name.trim()) return ElMessage.warning('请填写用户名')
  saving.value = true
  try {
    if (editing.value) await userApi.update(editing.value, body(form))
    else await userApi.create(body(form))
    dialog.value = false; await load(); emit('changed'); ElMessage.success('已保存，应用后生效')
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { saving.value = false }
}
async function toggle(row, enabled) {
  try { await userApi.update(row.name, body({ ...row, disabled: !enabled })); await load(); emit('changed') }
  catch (e) { ElMessage.error(apiError(e)) }
}
async function remove(row) {
  try { await ElMessageBox.confirm(`删除用户 ${row.name}？使用该用户的客户端将无法连接。`, '确认', { type: 'warning' }) } catch (_) { return }
  try { await userApi.remove(row.name); await load(); emit('changed') }
  catch (e) { ElMessage.error(apiError(e)) }
}

async function genUUID() {
  try { form.uuid = (await inboundApi.gen('uuid')).data.value } catch (e) { ElMessage.error(apiError(e)) }
}
async function genSS() {
  try { form.ss_key = (await inboundApi.gen('sskey', '2022-blake3-aes-256-gcm')).data.value } catch (e) { ElMessage.error(apiError(e)) }
}
function genPassword() {
  const a = new Uint8Array(12)
  crypto.getRandomValues(a)
  form.password = Array.from(a, (b) => 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'[b % 56]).join('')
}

// ---- 分享 ----
const shareDlg = ref(false)
const shareUser = ref(null)
const shareHost = ref('')
const shareItems = ref([])
const qr = reactive({})
async function openShare(row) {
  shareUser.value = row
  if (!shareHost.value) {
    try { shareHost.value = (await opsApi.settings()).data.public_host || window.location.hostname } catch (_) { shareHost.value = window.location.hostname }
  }
  shareDlg.value = true
  await loadShare()
}
async function loadShare() {
  try {
    const { data } = await userApi.share(shareUser.value.name, shareHost.value.trim())
    shareItems.value = data.items
    for (const it of data.items) qr[it.link] = await QRCode.toDataURL(it.link, { margin: 1, width: 180 })
  } catch (e) { shareItems.value = []; ElMessage.error(apiError(e)) }
}
async function copy(text) {
  try { await navigator.clipboard.writeText(text); ElMessage.success('已复制') } catch (_) { ElMessage.warning('复制失败，请手动复制') }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd">
        <span>用户 <span class="muted">共 {{ list.length }} 个 · 在线 {{ onlineCount }} 个</span></span>
        <el-button type="primary" @click="openCreate">+ 新建用户</el-button>
      </div>
    </template>
    <el-alert type="info" :closable="false" show-icon class="tip"
              title="一个用户可以关联多个入站：在 VLESS 入站用 UUID，在 Shadowsocks 入站用 SS 密钥，在 SOCKS/HTTP 入站用「用户名 + 密码」。流量按用户合并统计。" />
    <el-table :data="list" v-loading="loading" empty-text="还没有用户，点击右上角新建">
      <el-table-column label="用户" min-width="140"><template #default="{ row }">
        <div class="name"><span class="dot" :class="{ on: row.online }" :title="row.online ? '在线' : '离线'" /><b>{{ row.name }}</b></div>
        <div v-if="row.remark" class="muted">{{ row.remark }}</div>
      </template></el-table-column>
      <el-table-column label="启用" width="70"><template #default="{ row }">
        <el-switch :model-value="!row.disabled" size="small" @change="(v) => toggle(row, v)" />
      </template></el-table-column>
      <el-table-column label="关联入站" min-width="200"><template #default="{ row }">
        <template v-if="row.inbounds.length">
          <el-tag v-for="t in row.inbounds" :key="t" size="small" effect="plain" class="itag">
            {{ ibMap[t] ? ibLabel(ibMap[t]) : t }}</el-tag>
        </template>
        <span v-else class="warn-text">未关联入站，无法连接</span>
      </template></el-table-column>
      <el-table-column label="在线地址" min-width="140"><template #default="{ row }">
        <div v-for="ip in row.online_ips" :key="ip" class="ip">{{ ip }}</div>
        <span v-if="!row.online_ips.length" class="muted">—</span>
      </template></el-table-column>
      <el-table-column label="今日流量" width="110" align="right"><template #default="{ row }">
        {{ fmtBytes(row.today_up + row.today_down) }}</template></el-table-column>
      <el-table-column label="近 30 天" width="190" align="right"><template #default="{ row }">
        <div>{{ fmtBytes(row.month_up + row.month_down) }}</div>
        <div class="muted">↑ {{ fmtBytes(row.month_up) }} ↓ {{ fmtBytes(row.month_down) }}</div>
      </template></el-table-column>
      <el-table-column label="操作" width="200"><template #default="{ row }">
        <el-button size="small" :disabled="!row.inbounds.length" @click="openShare(row)">分享</el-button>
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button>
      </template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? `编辑用户 ${editing}` : '新建用户'" width="min(620px, 96vw)">
    <el-form label-width="90px">
      <el-form-item label="用户名">
        <el-input v-model="form.name" :disabled="!!editing" maxlength="32" placeholder="字母、数字、中文或 _ . -" />
        <div v-if="!editing" class="muted">创建后不能修改：它也是 SOCKS/HTTP 的账号，以及流量统计、在线识别的依据。</div>
      </el-form-item>
      <el-form-item label="备注"><el-input v-model="form.remark" maxlength="40" placeholder="可选，如 张三的手机" /></el-form-item>
      <el-form-item label="启用"><el-switch v-model="form.disabled" :active-value="false" :inactive-value="true" /></el-form-item>
      <el-form-item label="关联入站">
        <div style="width:100%">
          <el-checkbox-group v-model="form.inbounds" class="ibs">
            <el-checkbox v-for="ib in inbounds" :key="ib.tag" :value="ib.tag" border>
              {{ ibLabel(ib) }}<span v-if="ib.remark" class="muted proto">{{ PROTO[ib.protocol] }}</span>
            </el-checkbox>
          </el-checkbox-group>
          <div v-if="!inbounds.length" class="muted">还没有入站，请先到「入站」页创建。</div>
        </div>
      </el-form-item>
      <el-form-item label="连接凭据">
        <div class="creds">
          <div v-if="!shownCreds.length" class="muted">勾选入站后显示它需要的凭据{{ editing ? '' : '；留空会自动生成' }}。</div>
          <div v-for="c in shownCreds" :key="c.key" class="cred">
            <div class="cred-hd">
              <span class="cred-name">{{ c.label }}</span>
              <span class="muted">{{ c.usedBy.length ? '用于 ' + c.usedBy.join('、') : '未关联' + c.proto + ' 入站' }}</span>
            </div>
            <div class="cred-row">
              <el-input v-model="form[c.key]" class="mono" :placeholder="editing ? '' : '留空自动生成'" />
              <el-button-group>
                <el-button :disabled="!form[c.key]" @click="copy(form[c.key])">复制</el-button>
                <el-button @click="c.gen">重新生成</el-button>
              </el-button-group>
            </div>
          </div>
          <el-button v-if="otherCreds.length" link type="primary" size="small" @click="showCred = !showCred">
            {{ showCred ? '收起其他凭据' : `显示其他凭据（${otherCreds.map((c) => c.label).join('、')}）` }}</el-button>
          <div v-if="editing && shownCreds.length" class="muted">修改凭据后，客户端需要重新导入分享链接。</div>
        </div>
      </el-form-item>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
  </el-dialog>

  <el-dialog v-model="shareDlg" :title="`分享给 ${shareUser?.name || ''}`" width="min(720px, 96vw)">
    <div class="row" style="margin-bottom:12px">
      <span>服务器地址</span>
      <el-input v-model="shareHost" placeholder="客户端连接用的公网 IP 或域名" class="grow" @keyup.enter="loadShare" />
      <el-button @click="loadShare">生成</el-button>
    </div>
    <div v-for="it in shareItems" :key="it.link" class="share" :class="{ off: it.disabled }">
      <img v-if="qr[it.link]" :src="qr[it.link]" alt="二维码" class="qr" />
      <div class="grow">
        <b>{{ it.name }}</b>
        <el-tag v-if="it.disabled" size="small" type="info" style="margin-left:6px">用户已停用</el-tag>
        <el-tag v-if="it.local" size="small" type="warning" style="margin-left:6px">入站只监听本机</el-tag>
        <div class="link">{{ it.link }}</div>
        <el-button size="small" @click="copy(it.link)">复制链接</el-button>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped>
.hd { display:flex; justify-content:space-between; align-items:center; gap:8px; }
.muted { color:var(--el-text-color-secondary); font-size:12px; }
.tip { margin-bottom:12px; }
.name { display:flex; align-items:center; gap:6px; }
.dot { width:8px; height:8px; border-radius:50%; background:var(--el-border-color); flex-shrink:0; }
.dot.on { background:var(--el-color-success); }
.itag { margin:0 4px 2px 0; }
.ip { font:12px/20px ui-monospace, SFMono-Regular, Menlo, monospace; }
.warn-text { color:var(--el-color-warning); font-size:12px; }
.row { display:flex; gap:8px; align-items:center; width:100%; }
.grow { flex:1; min-width:120px; }
.ibs { display:flex; flex-wrap:wrap; gap:8px; }
.ibs :deep(.el-checkbox) { margin-right:0; }
.proto { margin-left:6px; }
.creds { width:100%; display:flex; flex-direction:column; gap:10px; padding:12px; border-radius:6px;
         background:var(--el-fill-color-light); border:1px solid var(--el-border-color-lighter); }
.cred { display:flex; flex-direction:column; gap:4px; }
.cred-hd { display:flex; align-items:baseline; gap:8px; line-height:20px; }
.cred-name { font-size:13px; font-weight:600; color:var(--el-text-color-regular); }
.cred-row { display:flex; gap:8px; }
.cred-row .el-input { flex:1; min-width:0; }
.mono :deep(input) { font-family:ui-monospace, SFMono-Regular, Menlo, monospace; font-size:12px; }
.creds > .el-button { align-self:flex-start; }
.share { display:flex; gap:14px; padding:10px 0; border-bottom:1px solid var(--el-border-color-lighter); }
.share.off { opacity:.5; }
.qr { width:140px; height:140px; border-radius:6px; background:#fff; }
.link { font:12px/1.5 ui-monospace, Menlo, monospace; word-break:break-all; color:var(--el-text-color-secondary); margin:6px 0; }
</style>
