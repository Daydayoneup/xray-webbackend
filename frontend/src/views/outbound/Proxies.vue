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
const editing = ref(null)
const mode = ref('manual')
const form = reactive({
  name: '', protocol: 'socks', host: '', port: null, user: '', pass: '',
  link: '', uuid: '', method: 'aes-256-gcm', network: 'tcp', tls: 'none',
  sni: '', path: '', wsHost: '', flow: '', fingerprint: 'chrome',
  publicKey: '', shortId: '', spiderX: '', allowInsecure: false,
})

const protocols = [
  { label: 'socks', value: 'socks' },
  { label: 'http', value: 'http' },
  { label: 'vmess', value: 'vmess' },
  { label: 'vless', value: 'vless' },
  { label: 'trojan', value: 'trojan' },
  { label: 'shadowsocks', value: 'shadowsocks' },
]

const ssMethods = ['aes-256-gcm', 'chacha20-ietf-poly1305', 'aes-128-gcm', 'none']
const networkOpts = ['tcp', 'ws', 'grpc', 'h2']
const tlsOpts = ['none', 'tls', 'reality']
const fpOpts = ['chrome', 'firefox', 'safari', 'edge', 'ios', 'android', 'random']

const isSimple = computed(() => form.protocol === 'socks' || form.protocol === 'http')
const isReality = computed(() => form.tls === 'reality')
const isTLS = computed(() => form.tls === 'tls')
const isWS = computed(() => form.network === 'ws')
const isGRPC = computed(() => form.network === 'grpc')
const isVless = computed(() => form.protocol === 'vless')
const isSS = computed(() => form.protocol === 'shadowsocks')

async function load() { list.value = (await proxyApi.list()).data }
onMounted(load)

function resetForm() {
  Object.assign(form, {
    name: '', protocol: 'socks', host: '', port: null, user: '', pass: '',
    link: '', uuid: '', method: 'aes-256-gcm', network: 'tcp', tls: 'none',
    sni: '', path: '', wsHost: '', flow: '', fingerprint: 'chrome',
    publicKey: '', shortId: '', spiderX: '', allowInsecure: false,
  })
  mode.value = 'manual'
}

function openCreate() {
  editing.value = null
  resetForm()
  dialog.value = true
}

function openEdit(row) {
  editing.value = row.tag
  Object.assign(form, {
    name: row.name, protocol: row.protocol, host: row.host || '', port: row.port || null,
    user: row.auth?.user || '', pass: row.auth?.pass || '', link: row.link || '',
    uuid: '', method: 'aes-256-gcm', network: 'tcp', tls: 'none',
    sni: '', path: '', wsHost: '', flow: '', fingerprint: 'chrome',
    publicKey: '', shortId: '', spiderX: '', allowInsecure: false,
  })
  mode.value = row.link ? 'link' : 'manual'
  dialog.value = true
}

function payload() {
  const p = { name: form.name.trim(), protocol: form.protocol }
  if (mode.value === 'link') {
    p.link = form.link.trim()
  } else {
    p.host = form.host.trim()
    p.port = form.port
    if (form.user.trim() || form.pass) p.auth = { user: form.user.trim(), pass: form.pass }
    if (!isSimple.value) {
      p.uuid = form.uuid.trim()
      p.network = form.network
      p.tls = form.tls
      p.sni = form.sni.trim()
      p.path = form.path.trim()
      p.ws_host = form.wsHost.trim()
      p.fingerprint = form.fingerprint
      p.allow_insecure = form.allowInsecure
      if (isVless.value) p.flow = form.flow.trim()
      if (isReality.value) {
        p.public_key = form.publicKey.trim()
        p.short_id = form.shortId.trim()
        p.spider_x = form.spiderX.trim()
      }
      if (isSS.value) p.method = form.method
    }
  }
  return p
}

async function save() {
  try {
    if (editing.value) await proxyApi.update(editing.value, payload())
    else await proxyApi.create(payload())
    dialog.value = false; await load(); await panel.refreshOutbounds(); emit('changed'); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(apiError(e)) }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`删除代理「${row.name}」?`, '确认', { type: 'warning' })
    await proxyApi.remove(row.tag); await load(); await panel.refreshAll(); emit('changed')
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(apiError(e))
  }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>自定义出口代理(落地代理)</span>
        <el-button type="primary" @click="openCreate">+ 新建代理</el-button></div>
    </template>
    <el-table :data="list">
      <el-table-column prop="tag" label="tag" width="80" />
      <el-table-column prop="name" label="名称" />
      <el-table-column prop="protocol" label="协议" width="110" />
      <el-table-column label="地址" width="200"><template #default="{ row }">
        <span v-if="row.host">{{ row.host }}:{{ row.port }}</span>
        <span v-else-if="row.link" style="color:var(--el-text-color-secondary);font-size:12px">{{ row.link.slice(0,40) }}…</span>
      </template></el-table-column>
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑代理' : '新建代理'" width="560px">
    <el-form label-width="100px">
      <el-form-item label="名称"><el-input v-model="form.name" placeholder="可选" /></el-form-item>
      <el-form-item label="协议"><el-select v-model="form.protocol">
        <el-option v-for="p in protocols" :key="p.value" :label="p.label" :value="p.value" />
      </el-select></el-form-item>

      <!-- Tab switch for complex protocols -->
      <el-tabs v-if="!isSimple" v-model="mode" class="mode-tabs">
        <el-tab-pane label="手动填写" name="manual" />
        <el-tab-pane label="粘贴链接" name="link" />
      </el-tabs>

      <!-- Link paste panel -->
      <template v-if="mode === 'link' && !isSimple">
        <el-form-item label="分享链接">
          <el-input v-model="form.link" type="textarea" :rows="3" placeholder="粘贴 vmess:// 或 vless:// 或 trojan:// 或 ss:// 链接" />
        </el-form-item>
      </template>

      <!-- Manual fill panel -->
      <template v-if="mode === 'manual' || isSimple">
        <el-form-item label="地址"><el-input v-model="form.host" placeholder="host" /></el-form-item>
        <el-form-item label="端口"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>

        <!-- socks/http auth -->
        <template v-if="isSimple">
          <el-form-item label="账号"><el-input v-model="form.user" placeholder="可选" /></el-form-item>
          <el-form-item label="密码"><el-input v-model="form.pass" placeholder="可选" /></el-form-item>
        </template>

        <!-- vmess/vless/trojan/ss manual fields -->
        <template v-if="!isSimple">
          <el-form-item v-if="isSS" label="加密方式">
            <el-select v-model="form.method">
              <el-option v-for="m in ssMethods" :key="m" :label="m" :value="m" />
            </el-select>
          </el-form-item>
          <el-form-item :label="isSS ? '密码' : 'UUID/密码'">
            <el-input v-model="form.uuid" :placeholder="isSS ? 'shadowsocks密码' : 'UUID或密码'" />
          </el-form-item>
          <el-form-item label="传输协议">
            <el-select v-model="form.network">
              <el-option v-for="n in networkOpts" :key="n" :label="n" :value="n" />
            </el-select>
          </el-form-item>
          <el-form-item label="TLS">
            <el-select v-model="form.tls">
              <el-option v-for="t in tlsOpts" :key="t" :label="t" :value="t" />
            </el-select>
          </el-form-item>

          <!-- Advanced: el-collapse -->
          <el-collapse style="margin-top:8px">
            <el-collapse-item title="高级配置" name="adv">
              <el-form-item v-if="isVless" label="Flow">
                <el-input v-model="form.flow" placeholder="xtls-rprx-vision" />
              </el-form-item>
              <el-form-item v-if="isTLS" label="SNI">
                <el-input v-model="form.sni" placeholder="默认同地址" />
              </el-form-item>
              <el-form-item v-if="isReality" label="SNI">
                <el-input v-model="form.sni" placeholder="reality 回落域名" />
              </el-form-item>
              <el-form-item :label="isGRPC ? 'ServiceName' : 'Path'">
                <el-input v-model="form.path" :placeholder="isGRPC ? 'grpc服务名' : '/ws-path'" />
              </el-form-item>
              <el-form-item v-if="isWS" label="Host">
                <el-input v-model="form.wsHost" placeholder="ws host header" />
              </el-form-item>
              <el-form-item v-if="isTLS || isReality" label="Fingerprint">
                <el-select v-model="form.fingerprint">
                  <el-option v-for="f in fpOpts" :key="f" :label="f" :value="f" />
                </el-select>
              </el-form-item>
              <el-form-item v-if="isReality" label="PublicKey">
                <el-input v-model="form.publicKey" placeholder="reality 公钥" />
              </el-form-item>
              <el-form-item v-if="isReality" label="ShortId">
                <el-input v-model="form.shortId" placeholder="shortId" />
              </el-form-item>
              <el-form-item v-if="isReality" label="SpiderX">
                <el-input v-model="form.spiderX" placeholder="spiderX" />
              </el-form-item>
              <el-form-item v-if="isTLS" label="AllowInsecure">
                <el-switch v-model="form.allowInsecure" />
              </el-form-item>
            </el-collapse-item>
          </el-collapse>
        </template>
      </template>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>
.hd { display:flex; justify-content:space-between; align-items:center; }
.mode-tabs { margin-bottom: 8px; }
</style>
