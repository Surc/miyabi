const subtitleLocalization = /^字幕[汉漢]化$/

export function isSubtitleLocalizationTag({ name, name_zht }: { name: string; name_zht?: string }) {
  return subtitleLocalization.test(name.trim()) || subtitleLocalization.test(name_zht?.trim() ?? '')
}
