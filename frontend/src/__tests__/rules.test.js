import { describe, it, expect } from 'vitest'
import { cleanRules, applyTemplate, ruleError, splitValues } from '../views/rules-helpers.js'

describe('rules helpers', () => {
  it('cleanRules drops empty value and keeps order', () => {
    const out = cleanRules([
      { type: 'full', value: 'a.com', outbound: 'direct', enabled: true },
      { type: 'full', value: '  ', outbound: 'direct', enabled: true },
      { type: 'geoip', value: 'cn', outbound: 'direct', enabled: false },
    ])
    expect(out.map((r) => r.value)).toEqual(['a.com', 'cn'])  // 空值丢弃,顺序不变
  })
  it('applyTemplate substitutes __PROXY__', () => {
    const tpl = [{ type: 'geosite', value: 'netflix', outbound: '__PROXY__' }]
    const out = applyTemplate(tpl, 'node-0')
    expect(out[0].outbound).toBe('node-0')
  })
  it('applyTemplate skips rules that already exist', () => {
    const tpl = [{ type: 'geoip', value: 'cn', outbound: 'direct' }, { type: 'geosite', value: 'cn', outbound: 'direct' }]
    const out = applyTemplate(tpl, 'x', [{ type: 'geoip', value: ' cn ', outbound: 'node-1' }])
    expect(out.map((r) => r.type)).toEqual(['geosite'])
  })
  it('splitValues & ruleError', () => {
    expect(splitValues('a.com, b.com  c.com')).toEqual(['a.com', 'b.com', 'c.com'])
    expect(ruleError({ type: 'port', value: '443, 1000-2000' })).toBe('')
    expect(ruleError({ type: 'port', value: 'abc' })).not.toBe('')
    expect(ruleError({ type: 'domain-suffix', value: 'https://a.com' })).not.toBe('')
    expect(ruleError({ type: 'ip', value: '10.0.0.0/8' })).toBe('')
  })
})

describe('rule names', () => {
  it('cleanRules keeps trimmed names', () => {
    const out = cleanRules([{ name: '  Netflix 走美国 ', type: 'geosite', value: 'netflix', outbound: 'x' }, { type: 'port', value: '25', outbound: 'block' }])
    expect(out.map((r) => r.name)).toEqual(['Netflix 走美国', ''])
  })
})
