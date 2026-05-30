import { describe, it, expect } from 'vitest'
import { latClass, latText, aliveStats } from '../utils/format.js'

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
  it('aliveStats', () => {
    const s = aliveStats([{ latency: 100, name: 'A' }, { latency: null }, { latency: 50, name: 'B' }])
    expect(s.alive).toBe(2)
    expect(s.fastest.name).toBe('B')
  })
})
