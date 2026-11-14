import { Children, isValidElement, useState, type ReactElement, type ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, expect, test, vi } from 'vitest'
import { useMDCzSettings, useUpdateMDCz, useTestMDCz, type MDCzSettings } from '@/api/mdcz'
import { MDCzSection } from '@/features/settings/extensions/mdcz-section'
import { MDCzConfigurationSection } from '@/features/settings/extensions/mdcz-configuration'

vi.mock('@/api/mdcz', () => ({
  useMDCzSettings: vi.fn(),
  useUpdateMDCz: vi.fn(),
  useTestMDCz: vi.fn()
}))
vi.mock('@/features/settings/extensions/mdcz-configuration', () => ({
  MDCzConfigurationSection: vi.fn(() => <div data-testid="advanced-configuration" />)
}))
vi.mock('react', async importOriginal => ({
  ...(await importOriginal<typeof import('react')>()),
  useState: vi.fn()
}))
const realReact = await vi.importActual<typeof import('react')>('react')
const defaults: MDCzSettings = {
  enabled: true,
  managed: true,
  mode: 'managed',
  status: 'configured',
  generation: 'first',
  server_url: 'http://127.0.0.1:3840',
  has_api_key: true,
  translate: false,
  badges: true,
  position: 'bottomLeft'
}
const save = { mutate: vi.fn(), reset: vi.fn(), isPending: false }
const connectionTest = { mutate: vi.fn(), reset: vi.fn(), isPending: false }
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(useState).mockImplementation(realReact.useState)
  vi.mocked(useMDCzSettings).mockReturnValue({ data: defaults } as ReturnType<
    typeof useMDCzSettings
  >)
  vi.mocked(useUpdateMDCz).mockReturnValue(save as unknown as ReturnType<typeof useUpdateMDCz>)
  vi.mocked(useTestMDCz).mockReturnValue(
    connectionTest as unknown as ReturnType<typeof useTestMDCz>
  )
})

type Element = ReactElement<{ children?: ReactNode; [key: string]: unknown }>
function all(node: ReactNode): Element[] {
  if (!isValidElement(node)) return []
  const element = node as Element
  return [element, ...Children.toArray(element.props.children).flatMap(all)]
}
function label(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  return isValidElement(node)
    ? Children.toArray((node as Element).props.children)
        .map(label)
        .join('')
    : ''
}
function find(tree: ReactNode, predicate: (element: Element) => boolean): Element {
  const element = all(tree).find(predicate)
  expect(element).toBeDefined()
  return element!
}
// Exercise component callbacks with retained state, without adding a browser or
// DOM dependency. SSR below still uses React's real hooks for child components.
function form() {
  const state: unknown[] = []
  return () => {
    let index = 0
    vi.mocked(useState).mockImplementation(((initial: unknown) => {
      const slot = index++
      if (!(slot in state)) state[slot] = typeof initial === 'function' ? initial() : initial
      return [
        state[slot],
        (value: unknown) => {
          state[slot] = typeof value === 'function' ? value(state[slot]) : value
        }
      ]
    }) as typeof useState)
    const tree = MDCzSection()
    vi.mocked(useState).mockImplementation(realReact.useState)
    return tree
  }
}
function changeMode(tree: ReactNode, value: 'native' | 'mdcz') {
  const select = find(tree, element => element.props['aria-label'] === '刮削模式')
  ;(select.props.onChange as (event: unknown) => void)({ target: { value } })
}
function saveButton(tree: ReactNode) {
  return find(tree, element => !!element.props.onClick && label(element) === '保存刮削设置')
}
function setDirty(tree: ReactNode, dirty: boolean) {
  const advanced = find(tree, element => element.type === MDCzConfigurationSection)
  ;(advanced.props.onDirtyChange as (dirty: boolean) => void)(dirty)
}

test('managed connection fields are read-only, the secret is never displayed, and native controls remain available', () => {
  const html = renderToStaticMarkup(<MDCzSection />)
  expect(html).toMatch(/type="url"[^>]*readOnly=""/i)
  expect(html).toMatch(/type="password"[^>]*disabled=""/)
  expect(html).toContain('连接地址和密钥由安装配置维护')
  expect(html).toContain('Miyabi 原版')
  expect(html).not.toContain('api_key')
})

test('legacy external connections remain editable', () => {
  vi.mocked(useMDCzSettings).mockReturnValue({
    data: { ...defaults, managed: false, mode: 'external' }
  } as ReturnType<typeof useMDCzSettings>)
  const html = renderToStaticMarkup(<MDCzSection />)
  expect(html).not.toMatch(/readOnly=""/i)
  expect(html).not.toMatch(/type="password"[^>]*disabled=""/)
})

test('bundled WebUI has a settings entry before enhanced scraping is enabled', () => {
  vi.mocked(useMDCzSettings).mockReturnValue({
    data: { ...defaults, enabled: false, web_port: 3838 }
  } as ReturnType<typeof useMDCzSettings>)
  const html = renderToStaticMarkup(<MDCzSection />)
  expect(html).toContain('打开 MDCz 配置')
  expect(html).not.toContain('advanced-configuration')
  expect(html).not.toContain('翻译标题与介绍')
})

test('an unavailable bridge does not block saving the switch back to native mode', () => {
  vi.mocked(useMDCzSettings).mockReturnValue({
    data: { ...defaults, status: 'unavailable', server_url: '', has_api_key: false }
  } as ReturnType<typeof useMDCzSettings>)
  const render = form()
  changeMode(render(), 'native')
  const tree = render()
  expect(saveButton(tree).props.disabled).toBe(false)
  ;(saveButton(tree).props.onClick as () => void)()
  expect(save.mutate).toHaveBeenCalledWith(
    expect.objectContaining({ enabled: false }),
    expect.any(Object)
  )
  expect(connectionTest.mutate).not.toHaveBeenCalled()
  expect(renderToStaticMarkup(tree)).toContain('媒体库和原版模式仍可使用')
})

test('advanced configuration drafts keep the editor open and block engine save until explicit discard/save', () => {
  const render = form()
  setDirty(render(), true)
  changeMode(render(), 'native')
  let tree = render()
  expect(saveButton(tree).props.disabled).toBe(true)
  const advancedDetails = find(
    tree,
    element => element.type === 'details' && label(element).includes('MDCz 增强设置')
  )
  expect(advancedDetails.props.open).toBe(true)
  expect(renderToStaticMarkup(tree)).toContain('请先保存或放弃')
  expect(save.mutate).not.toHaveBeenCalled()
  setDirty(tree, false)
  tree = render()
  expect(saveButton(tree).props.disabled).toBe(false)
  ;(saveButton(tree).props.onClick as () => void)()
  expect(save.mutate).toHaveBeenCalledTimes(1)
})
