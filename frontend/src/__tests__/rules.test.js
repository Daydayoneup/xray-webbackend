import { describe, it, expect } from 'vitest'
import { cleanRules, applyTemplate } from '../views/rules-helpers.js'

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
})
