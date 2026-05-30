export const RULE_TYPES = [
  ['domain-suffix', '域名后缀'], ['full', '完整域名'], ['keyword', '关键字'],
  ['geosite', '预置集合'], ['ip', 'IP段'], ['geoip', '地区IP(如cn)'], ['port', '端口'],
]

export function cleanRules(rules) {
  return rules
    .filter((r) => (r.value || '').trim())
    .map((r) => ({ type: r.type, value: r.value.trim(), outbound: r.outbound, enabled: r.enabled !== false }))
}

export function applyTemplate(tpl, proxyTag) {
  return tpl.map((r) => ({ type: r.type, value: r.value, enabled: true,
    outbound: r.outbound === '__PROXY__' ? proxyTag : r.outbound }))
}
