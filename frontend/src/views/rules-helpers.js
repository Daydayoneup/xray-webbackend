export const RULE_TYPES = [
  ['domain-suffix', '域名后缀'], ['full', '完整域名'], ['keyword', '关键字'],
  ['geosite', '预置域名集合'], ['ip', 'IP / CIDR'], ['geoip', '地区IP'], ['port', '端口'],
  ['set', '自定义分组'],
]

export const RULE_PLACEHOLDER = {
  'domain-suffix': 'google.com, youtube.com',
  full: 'www.example.com',
  keyword: 'google',
  geosite: 'cn / netflix / category-ads-all',
  ip: '8.8.8.8, 10.0.0.0/8',
  geoip: 'cn / jp / private',
  port: '443 或 1000-2000',
  set: '选择一个分组',
}

export const DOMAIN_STRATEGIES = [
  ['IPIfNonMatch', '域名未命中时解析 IP 再匹配（推荐）'],
  ['AsIs', '只按域名匹配（IP 规则对域名请求无效）'],
  ['IPOnDemand', '遇到 IP 规则立即解析'],
]

export function splitValues(v) {
  return (v || '').split(/[,\s]+/).map((x) => x.trim()).filter(Boolean)
}

const portRe = /^\d{1,5}(-\d{1,5})?$/
const ipRe = /^[0-9a-fA-F:.]+(\/\d{1,3})?$/

// 前端即时校验，与后端 ValidateRuleValue 保持一致；返回错误文案或 ''
export function ruleError(r) {
  const vals = splitValues(r.value)
  if (!vals.length || r.type === 'set') return ''
  for (const v of vals) {
    if (r.type === 'port' && !portRe.test(v)) return `端口格式错误: ${v}`
    if (r.type === 'ip' && !ipRe.test(v)) return `IP 格式错误: ${v}`
    if (['domain-suffix', 'full', 'keyword'].includes(r.type) && /[/:]/.test(v)) return `请只填域名: ${v}`
  }
  return ''
}

export function cleanRules(rules) {
  return rules
    .filter((r) => (r.value || '').trim())
    .map((r) => ({ name: (r.name || '').trim(), type: r.type, value: r.value.trim(), outbound: r.outbound, enabled: r.enabled !== false }))
}

// 套用模板：__PROXY__ 替换为代理出口；已存在的同类型同值规则不重复添加
export function applyTemplate(tpl, proxyTag, existing = []) {
  const key = (r) => `${r.type}|${splitValues(r.value).join(',')}`
  const have = new Set(existing.map(key))
  return tpl
    .map((r) => ({ type: r.type, value: r.value, enabled: true,
      outbound: r.outbound === '__PROXY__' ? proxyTag : r.outbound }))
    .filter((r) => !have.has(key(r)))
}
