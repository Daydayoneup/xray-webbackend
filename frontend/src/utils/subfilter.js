// 与后端 service.DefaultExclude / Keywords / FilterNodes 保持一致
export const DEFAULT_EXCLUDE = '剩余|到期|过期|官网|网址|套餐|重置|expire'

export function keywords(s) {
  return (s || '').split(/[|,，\n]/).map((k) => k.trim().toLowerCase()).filter(Boolean)
}

// 预览：按排除关键字，names 中哪些会被排除
export function previewExcluded(names, exclude) {
  const kws = keywords(exclude)
  if (!kws.length) return []
  return names.filter((n) => kws.some((k) => n.toLowerCase().includes(k)))
}
