import { describe, it, expect } from 'vitest'
import { latClass, latText, aliveStats, fmtBytes, fmtAgo, expireInfo, subName } from '../utils/format.js'

describe('format', () => {
  it('latClass', () => {
    expect(latClass(null)).toBe('bad')
    expect(latClass(100)).toBe('good')
    expect(latClass(200)).toBe('mid')   // 边界
    expect(latClass(300)).toBe('mid')
    expect(latClass(500)).toBe('bad')   // 边界
    expect(latClass(900)).toBe('bad')
  })
  it('latText', () => {
    expect(latText(null)).toBe('超时')
    expect(latText(88)).toBe('88ms')
  })
  it('aliveStats ignores disabled/missing nodes', () => {
    const s = aliveStats([{ latency: 100, name: 'A' }, { latency: null }, { latency: 50, name: 'B' },
      { latency: 10, name: 'C', disabled: true }, { latency: 5, name: 'D', missing: true }])
    expect(s.alive).toBe(2)
    expect(s.fastest.name).toBe('B')
  })
  it('fmtBytes / fmtAgo / expireInfo / subName', () => {
    expect(fmtBytes(1024 ** 3 * 1.5)).toBe('1.50 GB')
    expect(fmtAgo(0)).toBe('从未')
    expect(fmtAgo(1000, 1000 + 7200)).toBe('2 小时前')
    expect(expireInfo(100, 200).level).toBe('danger')
    expect(expireInfo(200 + 3 * 86400, 200).level).toBe('warn')
    expect(subName({ id: 3, name: '', remarks: '' })).toBe('订阅 3')
    expect(subName({ id: 3, name: '我的', remarks: 'x' })).toBe('我的')
  })
})

import { fmtDur, isPrivateIP } from '../utils/format.js'
describe('online helpers', () => {
  it('fmtDur', () => {
    expect(fmtDur(30)).toBe('不到 1 分钟')
    expect(fmtDur(150)).toBe('2 分钟')
    expect(fmtDur(3900)).toBe('1 小时 5 分钟')
  })
  it('isPrivateIP', () => {
    expect(isPrivateIP('192.168.1.2')).toBe(true)
    expect(isPrivateIP('172.20.0.1')).toBe(true)
    expect(isPrivateIP('127.0.0.1')).toBe(true)
    expect(isPrivateIP('8.8.8.8')).toBe(false)
    expect(isPrivateIP('172.32.0.1')).toBe(false)
    expect(isPrivateIP('fd00::1')).toBe(true)
    expect(isPrivateIP('2001:db8::1')).toBe(false)
  })
})
