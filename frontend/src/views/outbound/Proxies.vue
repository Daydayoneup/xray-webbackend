<script setup>
import { ref, onMounted, reactive, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { proxyApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const list = ref([])
const dialog = ref(false)
const saving = ref(false)
const editing = ref(null)
const mode = ref('manual')
const advOpen = ref([])

const EMPTY = {
  name: '', protocol: 'socks', host: '', port: null, user: '', pass: '', via: '',
  link: '', uuid: '', method: 'aes-256-gcm', network: 'tcp', tls: 'none',
  sni: '', path: '', wsHost: '', xmode: '', alpn: '', flow: '', fingerprint: 'chrome',
  publicKey: '', shortId: '', spiderX: '',
}
const form = reactive({ ...EMPTY })

const protocols = ['socks', 'http', 'vmess', 'vless', 'trojan', 'shadowsocks']
const ssMethods = ['aes-256-gcm', 'aes-128-gcm', 'chacha20-ietf-poly1305', 'xchacha20-ietf-poly1305',
  '2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305']  // 新版 Xray 不支持 none
const networkOpts = [['tcp', 'tcp'], ['ws', 'websocket'], ['grpc', 'gRPC'], ['h2', 'HTTP/2'], ['httpupgrade', 'HTTPUpgrade'], ['xhttp', 'XHTTP']]
const fpOpts = ['chrome', 'firefox', 'safari', 'edge', 'ios', 'android', 'random', 'randomized']

const isSimple = computed(() => form.protocol === 'socks' || form.protocol === 'http')
const isSS = computed(() => form.protocol === 'shadowsocks')
const isVless = computed(() => form.protocol === 'vless')
const isReality = computed(() => form.tls === 'reality')
const isTLS = computed(() => form.tls === 'tls')
const isGRPC = computed(() => form.network === 'grpc')
const hasPath = computed(() => ['ws', 'h2', 'httpupgrade', 'xhttp', 'grpc'].includes(form.network))
const hasHost = computed(() => ['ws', 'h2', 'httpupgrade', 'xhttp'].includes(form.network))
const tlsOpts = computed(() => form.protocol === 'trojan' ? ['tls', 'reality'] : ['none', 'tls', 'reality'])

// 链式代理的前置出口：节点或其他自定义代理（不能是自己）
const viaOpts = computed(() => panel.outbounds.filter((o) => (o.kind === 'node' || o.kind === 'proxy') && o.tag !== editing.value))

async function load() { list.value = (await proxyApi.list()).data; await panel.refreshOutbounds() }
onMounted(load)

function openCreate() {
  editing.value = null
  Object.assign(form, EMPTY); mode.value = 'manual'; advOpen.value = []
  dialog.value = true
}

function openEdit(row) {
  editing.value = row.tag
  const s = row.spec || {}
  Object.assign(form, EMPTY, {
    name: row.name, protocol: s.protocol || row.protocol, host: s.host || row.host || '', port: s.port || row.port || null,
    user: s.user || row.auth?.user || '', pass: s.pass || row.auth?.pass || '', via: row.via || '', link: row.link || '',
    uuid: s.uuid || '', method: s.method || EMPTY.method, network: s.network || 'tcp',
    tls: s.security || (s.protocol === 'trojan' ? 'tls' : 'none'), sni: s.sni || '', path: s.path || '',
    wsHost: s.host_header || '', xmode: s.mode || '', alpn: s.alpn || '', flow: s.flow || '',
    fingerprint: s.fingerprint || '', publicKey: s.public_key || '', shortId: s.short_id || '', spiderX: s.spider_x || '',
  })
  mode.value = row.link ? 'link' : 'manual'
  advOpen.value = (form.flow || form.sni || form.path || form.wsHost || form.alpn || isReality.value) ? ['adv'] : []
  dialog.value = true
}

function onProtocolChange(p) {
  if (p === 'trojan' && form.tls === 'none') form.tls = 'tls'
  if (p === 'socks' || p === 'http') mode.value = 'manual'
}

function payload() {
  const p = { name: form.name.trim(), protocol: form.protocol, via: form.via }
  if (mode.value === 'link' && !isSimple.value) {
    p.link = form.link.trim()
    return p
  }
  p.host = form.host.trim()
  p.port = form.port
  if (isSimple.value) {
    if (form.user.trim() || form.pass) p.auth = { user: form.user.trim(), pass: form.pass }
    return p
  }
  p.uuid = form.uuid.trim()
  if (isSS.value) { p.method = form.method; return p }
  Object.assign(p, {
    network: form.network, tls: form.tls, sni: form.sni.trim(), fingerprint: form.fingerprint,
    path: hasPath.value ? form.path.trim() : '', ws_host: hasHost.value ? form.wsHost.trim() : '',
    mode: form.xmode, alpn: isTLS.value ? form.alpn.trim() : '',
  })
  if (isVless.value) p.flow = form.flow.trim()
  if (isReality.value) Object.assign(p, { public_key: form.publicKey.trim(), short_id: form.shortId.trim(), spider_x: form.spiderX.trim() })
  return p
}

async function save() {
  saving.value = true
  try {
    if (editing.value) await proxyApi.update(editing.value, payload())
    else await proxyApi.create(payload())
    dialog.value = false; await load(); await panel.refreshAll(); emit('changed'); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { saving.value = false }
}

async function remove(row) {
  const refs = panel.outboundMap[row.tag]?.refs || 0
  const msg = refs
    ? `代理「${row.name}」正被 ${refs} 处规则/节点组/默认出口使用，删除后相关规则会被移除。确定删除？`
    : `删除代理「${row.name}」？`
  try { await ElMessageBox.confirm(msg, '确认删除', { type: 'warning', confirmButtonText: '删除' }) } catch (_) { return }
  try { await proxyApi.remove(row.tag); await load(); await panel.refreshAll(); emit('changed') }
  catch (e) { ElMessage.error(apiError(e)) }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>自定义代理（手动添加的代理服务器）</span>
        <el-button type="primary" @click="openCreate">+ 新建代理</el-button></div>
    </template>
    <p class="muted" style="margin-top:0">添加自建或购买的代理服务器（区别于从机场订阅自动拉取的节点）。可选设置「前置出口」组成链式代理：本机 → 前置节点 → 本代理 → 目标网站，目标网站看到的是本代理的 IP（即常说的“落地”）。</p>
    <el-table :data="list">
      <el-table-column prop="name" label="名称" min-width="140">
        <template #default="{ row }">
          {{ row.name }}
          <el-tooltip v-if="panel.outboundMap[row.tag] && !panel.outboundMap[row.tag].available" :content="panel.outboundMap[row.tag].reason">
            <el-tag type="danger" size="small">不可用</el-tag>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column prop="protocol" label="协议" width="110" />
      <el-table-column label="地址" min-width="160"><template #default="{ row }">{{ row.host }}:{{ row.port }}</template></el-table-column>
      <el-table-column label="前置出口" min-width="140"><template #default="{ row }">
        <span v-if="row.via">→ {{ panel.label(row.via) }}</span><span v-else class="muted">直连</span>
      </template></el-table-column>
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑代理' : '新建代理'" width="min(580px, 96vw)">
    <el-form label-width="100px">
      <el-form-item label="名称"><el-input v-model="form.name" placeholder="可选，留空则使用链接中的名称" /></el-form-item>
      <el-form-item label="协议"><el-select v-model="form.protocol" @change="onProtocolChange">
        <el-option v-for="p in protocols" :key="p" :label="p" :value="p" />
      </el-select></el-form-item>
      <el-form-item label="前置出口">
        <el-select v-model="form.via" clearable filterable placeholder="不设置（服务器直接连接）" style="width:100%">
          <el-option v-for="o in viaOpts" :key="o.tag" :label="o.label" :value="o.tag" :disabled="!o.available" />
        </el-select>
      </el-form-item>

      <el-tabs v-if="!isSimple" v-model="mode" class="mode-tabs">
        <el-tab-pane label="手动填写" name="manual" />
        <el-tab-pane label="粘贴链接" name="link" />
      </el-tabs>

      <template v-if="mode === 'link' && !isSimple">
        <el-form-item label="分享链接">
          <el-input v-model="form.link" type="textarea" :rows="3" placeholder="粘贴 vmess:// vless:// trojan:// ss:// 链接" />
        </el-form-item>
      </template>

      <template v-else>
        <el-form-item label="地址" required><el-input v-model="form.host" placeholder="域名或 IP" /></el-form-item>
        <el-form-item label="端口" required><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>

        <template v-if="isSimple">
          <el-form-item label="账号"><el-input v-model="form.user" placeholder="可选" /></el-form-item>
          <el-form-item label="密码"><el-input v-model="form.pass" type="password" show-password placeholder="可选" /></el-form-item>
        </template>

        <template v-else>
          <el-form-item v-if="isSS" label="加密方式">
            <el-select v-model="form.method" filterable allow-create>
              <el-option v-for="m in ssMethods" :key="m" :label="m" :value="m" />
            </el-select>
          </el-form-item>
          <el-form-item :label="isSS || form.protocol === 'trojan' ? '密码' : 'UUID'" required>
            <el-input v-model="form.uuid" :type="isSS || form.protocol === 'trojan' ? 'password' : 'text'" :show-password="isSS || form.protocol === 'trojan'"
                      :placeholder="isSS || form.protocol === 'trojan' ? '密码' : 'xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx'" />
          </el-form-item>
          <template v-if="!isSS">
            <el-form-item label="传输协议">
              <el-select v-model="form.network">
                <el-option v-for="[v, l] in networkOpts" :key="v" :label="l" :value="v" />
              </el-select>
            </el-form-item>
            <el-form-item label="安全">
              <el-radio-group v-model="form.tls">
                <el-radio-button v-for="t in tlsOpts" :key="t" :value="t">{{ t }}</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <template v-if="isReality">
              <el-form-item label="PublicKey" required><el-input v-model="form.publicKey" placeholder="服务端 reality 公钥 (pbk)" /></el-form-item>
              <el-form-item label="SNI"><el-input v-model="form.sni" placeholder="伪装域名，如 www.microsoft.com" /></el-form-item>
            </template>

            <el-collapse v-model="advOpen" style="margin-top:8px">
              <el-collapse-item title="高级配置" name="adv">
                <el-form-item v-if="isVless" label="Flow">
                  <el-select v-model="form.flow" clearable allow-create filterable placeholder="不设置">
                    <el-option label="xtls-rprx-vision" value="xtls-rprx-vision" />
                  </el-select>
                </el-form-item>
                <el-form-item v-if="isTLS" label="SNI"><el-input v-model="form.sni" placeholder="默认同地址" /></el-form-item>
                <el-form-item v-if="hasPath" :label="isGRPC ? 'ServiceName' : 'Path'">
                  <el-input v-model="form.path" :placeholder="isGRPC ? 'gRPC 服务名' : '/path'" />
                </el-form-item>
                <el-form-item v-if="hasHost" label="Host"><el-input v-model="form.wsHost" placeholder="Host 请求头（可选）" /></el-form-item>
                <el-form-item v-if="form.network === 'xhttp'" label="XHTTP 模式">
                  <el-select v-model="form.xmode" clearable placeholder="auto">
                    <el-option v-for="m in ['auto', 'packet-up', 'stream-up', 'stream-one']" :key="m" :label="m" :value="m" />
                  </el-select>
                </el-form-item>
                <el-form-item v-if="isGRPC" label="gRPC 模式">
                  <el-radio-group v-model="form.xmode"><el-radio-button value="">gun</el-radio-button><el-radio-button value="multi">multi</el-radio-button></el-radio-group>
                </el-form-item>
                <el-form-item v-if="isTLS || isReality" label="Fingerprint">
                  <el-select v-model="form.fingerprint">
                    <el-option label="不设置" value="" />
                    <el-option v-for="f in fpOpts" :key="f" :label="f" :value="f" />
                  </el-select>
                </el-form-item>
                <el-form-item v-if="isTLS" label="ALPN"><el-input v-model="form.alpn" placeholder="如 h2,http/1.1（可选）" /></el-form-item>
                <template v-if="isReality">
                  <el-form-item label="ShortId"><el-input v-model="form.shortId" placeholder="sid（可选）" /></el-form-item>
                  <el-form-item label="SpiderX"><el-input v-model="form.spiderX" placeholder="spx（可选）" /></el-form-item>
                </template>
              </el-collapse-item>
            </el-collapse>
          </template>
        </template>
      </template>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>
.hd { display:flex; justify-content:space-between; align-items:center; }
.mode-tabs { margin-bottom: 8px; }
</style>
