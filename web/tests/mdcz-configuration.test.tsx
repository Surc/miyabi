import { Children, isValidElement, useState, type ReactElement, type ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, expect, test, vi } from 'vitest'
import {
  useMDCzConfiguration,
  useUpdateMDCzConfiguration,
  type Configuration
} from '@/api/mdcz-configuration'
import { MDCzConfigurationSection } from '@/features/settings/extensions/mdcz-configuration'

vi.mock('@/api/mdcz-configuration', () => ({
  useMDCzConfiguration: vi.fn(),
  useUpdateMDCzConfiguration: vi.fn()
}))
vi.mock('react', async importOriginal => ({
  ...(await importOriginal<typeof import('react')>()),
  useState: vi.fn()
}))
const realReact = await vi.importActual<typeof import('react')>('react')
const configuration: Configuration = {
  configuration_id: 'configuration-before-outage',
  connection_generation: 'original-bridge',
  configuration_version: 1,
  available_sites: ['javdb'],
  values: {
    sites: ['javdb'],
    direct_sites: [],
    use_proxy: false,
    timeout: 30,
    retries: 2,
    javdb_url: 'https://javdb.com',
    javbus_url: 'https://www.javbus.com',
    translation_engine: 'google',
    target_language: 'zh-CN',
    baidu_service: 'general',
    baidu_app_id: '',
    llm_base_url: '',
    llm_model: '',
    llm_api_format: 'responses',
    llm_service_type: 'openai-compatible'
  },
  secrets: {
    proxy_url: false,
    javdb_cookie: true,
    javbus_cookie: false,
    fantia_cookie: false,
    deepl_key: false,
    baidu_secret: false,
    baidu_key: false,
    llm_key: false
  }
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(useState).mockImplementation(realReact.useState)
  vi.mocked(useMDCzConfiguration).mockReturnValue({ data: configuration } as ReturnType<
    typeof useMDCzConfiguration
  >)
  vi.mocked(useUpdateMDCzConfiguration).mockReturnValue({
    reset: vi.fn(),
    mutate: vi.fn()
  } as unknown as ReturnType<typeof useUpdateMDCzConfiguration>)
})

test('connection loss keeps the loaded advanced editor and explicit discard action available', () => {
  const html = renderToStaticMarkup(
    <MDCzConfigurationSection connected={false} disabled={false} onDirtyChange={vi.fn()} />
  )
  expect(html).toContain('已加载的配置草稿会保留')
  expect(html).toContain('保存来源与翻译配置')
  expect(html).toContain('重新读取')
  expect(html).toContain('https://javdb.com')
  expect(html).toContain('已保存凭据')
  expect(vi.mocked(useMDCzConfiguration)).toHaveBeenCalledWith(false)
})

test('an unavailable connection with no cached configuration does not fabricate an editor or loading state', () => {
  vi.mocked(useMDCzConfiguration).mockReturnValue({ isPending: true } as ReturnType<
    typeof useMDCzConfiguration
  >)
  const html = renderToStaticMarkup(
    <MDCzConfigurationSection connected={false} disabled={false} onDirtyChange={vi.fn()} />
  )
  expect(html).toContain('当前连接不可用')
  expect(html).not.toContain('保存来源与翻译配置')
  expect(html).not.toContain('正在读取引擎配置')
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

test('a background configuration read cannot retarget an existing draft; explicit discard starts a new generation', () => {
  const mutate = vi.fn()
  vi.mocked(useUpdateMDCzConfiguration).mockReturnValue({
    mutate,
    reset: vi.fn()
  } as unknown as ReturnType<typeof useUpdateMDCzConfiguration>)
  const reload = vi.fn(async () => undefined)
  vi.mocked(useMDCzConfiguration).mockReturnValue({
    data: configuration,
    refetch: reload
  } as unknown as ReturnType<typeof useMDCzConfiguration>)
  let editor!: Element
  function Capture() {
    const tree = MDCzConfigurationSection({
      connected: true,
      disabled: false,
      onDirtyChange: vi.fn()
    })
    editor = find(tree, element => element.props.initial === configuration)
    return tree
  }
  renderToStaticMarkup(<Capture />)
  const state: unknown[] = []
  const render = (initial: Configuration) => {
    let index = 0
    vi.mocked(useState).mockImplementation(((value: unknown) => {
      const slot = index++
      if (!(slot in state)) state[slot] = typeof value === 'function' ? value() : value
      return [
        state[slot],
        (next: unknown) => {
          state[slot] = typeof next === 'function' ? next(state[slot]) : next
        }
      ]
    }) as typeof useState)
    const tree = (editor.type as (props: Record<string, unknown>) => ReactElement)({
      ...editor.props,
      initial
    })
    vi.mocked(useState).mockImplementation(realReact.useState)
    return tree
  }
  const edit = (tree: ReactNode, oldValue: string, next: string) => {
    const input = find(tree, element => element.props.value === oldValue)
    ;(input.props.onChange as (event: unknown) => void)({ target: { value: next } })
  }
  const click = (tree: ReactNode, text: string) => {
    const button = find(tree, element => !!element.props.onClick && label(element) === text)
    ;(button.props.onClick as () => void)()
  }
  edit(render(configuration), configuration.values.javdb_url, 'https://edited.example')
  const newer = {
    ...configuration,
    configuration_id: 'newer-profile',
    connection_generation: 'different-bridge'
  }
  edit(render(newer), configuration.values.javbus_url, 'https://edited-bus.example')
  click(render(newer), '保存来源与翻译配置')
  expect(mutate).toHaveBeenLastCalledWith(
    expect.objectContaining({
      configuration_id: configuration.configuration_id,
      connection_generation: configuration.connection_generation,
      values: expect.objectContaining({
        javdb_url: 'https://edited.example',
        javbus_url: 'https://edited-bus.example'
      })
    }),
    expect.any(Object)
  )
  click(render(newer), '放弃修改并重读')
  expect(reload).toHaveBeenCalledTimes(1)
  edit(render(newer), configuration.values.javdb_url, 'https://fresh-edit.example')
  click(render(newer), '保存来源与翻译配置')
  expect(mutate).toHaveBeenLastCalledWith(
    expect.objectContaining({
      configuration_id: newer.configuration_id,
      connection_generation: newer.connection_generation
    }),
    expect.any(Object)
  )
  // A stale pre-upgrade response lacking authority must stay missing so the
  // server rejects it, never silently adopt the newly observed destination.
  state.length = 0
  const legacy = { ...configuration, connection_generation: undefined } as unknown as Configuration
  edit(render(legacy), configuration.values.javdb_url, 'https://legacy-edit.example')
  edit(render(newer), configuration.values.javbus_url, 'https://later-edit.example')
  click(render(newer), '保存来源与翻译配置')
  expect(mutate).toHaveBeenLastCalledWith(
    expect.objectContaining({ connection_generation: undefined }),
    expect.any(Object)
  )
})
