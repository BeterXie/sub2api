import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PluginsView from '../PluginsView.vue'

const {
  listPlugins,
  uploadPlugin,
  enablePlugin,
  savePluginConfig,
  listPluginAccounts,
  testPlugin,
  createUISession,
  stepUpRun,
} = vi.hoisted(() => ({
  listPlugins: vi.fn(),
  uploadPlugin: vi.fn(),
  enablePlugin: vi.fn(),
  savePluginConfig: vi.fn(),
  listPluginAccounts: vi.fn(),
  testPlugin: vi.fn(),
  createUISession: vi.fn(),
  stepUpRun: vi.fn((action: () => Promise<unknown>) => action()),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    plugins: {
      list: listPlugins,
      upload: uploadPlugin,
      enable: enablePlugin,
      disable: vi.fn(),
      remove: vi.fn(),
      getConfig: vi.fn().mockResolvedValue({}),
      accounts: listPluginAccounts,
      saveConfig: savePluginConfig,
      test: testPlugin,
      createUISession,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
  }),
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

const plugin = {
  id: 7,
  plugin_key: 'local.test.transport',
  name: 'Test Transport',
  version: '1.0.0',
  description: '',
  author: 'test',
  manifest: {
    schema_version: 1,
    id: 'local.test.transport',
    name: 'Test Transport',
    version: '1.0.0',
    requires: {
      sub2api: '>=0.1.0',
      plugin_protocol: 1,
      transport_api: 1,
      ui_bridge: 1,
    },
    capabilities: [],
    ui: { entrypoint: 'ui/index.html' },
  },
  binary_sha256: 'a'.repeat(64),
  signature_status: 'trusted' as const,
  state: 'disabled' as const,
  last_error: '',
  installed_at: '2026-08-22T00:00:00Z',
  updated_at: '2026-08-22T00:00:00Z',
  bindings: [
    {
      id: 1,
      plugin_id: 7,
      capability: 'openai.oauth.outbound_transport.v1',
      platform: 'openai',
      account_type: 'oauth',
      enabled: false,
      rollout_percent: 100,
    },
  ],
  compatibility: {
    compatible: true,
    tested: true,
    status: 'compatible' as const,
    message: '',
    current_sub2api_version: '0.1.0',
    required_sub2api_version: '>=0.1.0',
    recommended_sub2api_version: '0.1.0',
    plugin_protocol: 1,
    transport_api: 1,
    ui_bridge: 1,
  },
  runtime_healthy: false,
  runtime_message: '',
}

function mountView(attachTo?: HTMLElement) {
  return mount(PluginsView, {
    attachTo,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: { template: '<div><slot /></div>' },
        Icon: true,
        TotpStepUpDialog: true,
      },
    },
  })
}

describe('管理员插件页二次验证', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stepUpRun.mockImplementation((action: () => Promise<unknown>) => action())
    listPlugins.mockResolvedValue([plugin])
    uploadPlugin.mockResolvedValue(plugin)
    enablePlugin.mockResolvedValue(plugin)
    savePluginConfig.mockResolvedValue({ enabled: true })
    listPluginAccounts.mockResolvedValue([{ id: 2, name: 'OAuth account', platform: 'openai', account_type: 'oauth', status: 'active', schedulable: true }])
    testPlugin.mockResolvedValue({ success: true, message: 'ok', latency_ms: 1 })
    createUISession.mockResolvedValue({
      url: '/api/v1/plugin-ui/token/index.html#bridge_token=bridge',
      bridge_token: 'bridge',
      ui_bridge_version: 1,
      expires_at: '2026-08-22T01:00:00Z',
    })
  })

  it('启用插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.enable'))
    expect(button).toBeDefined()
    await button!.trigger('click')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(enablePlugin).toHaveBeenCalledWith(7, 100, false)
  })

  it('上传插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', {
      configurable: true,
      value: [new File(['plugin'], 'transport.s2plugin', { type: 'application/zip' })],
    })

    await input.trigger('change')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(uploadPlugin).toHaveBeenCalledTimes(1)
  })

  async function openPluginUI() {
    const wrapper = mountView(document.body)
    await flushPromises()
    const button = wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.configure'))
    await button!.trigger('click')
    await flushPromises()
    const frame = wrapper.get('iframe').element as HTMLIFrameElement
    const postMessage = vi.spyOn(frame.contentWindow!, 'postMessage')
    return { wrapper, frame, postMessage }
  }

  function sendBridgeMessage(frame: HTMLIFrameElement, type: string) {
    window.dispatchEvent(new MessageEvent('message', {
      source: frame.contentWindow,
      origin: 'null',
      data: { source: 'sub2api-plugin-ui', bridge_token: 'bridge', request_id: 'test-request', type },
    }))
  }

  it('插件配置页可以读取账号选项，不触发二次验证', async () => {
    const { wrapper, frame, postMessage } = await openPluginUI()
    sendBridgeMessage(frame, 'accounts.list')
    await flushPromises()

    expect(listPluginAccounts).toHaveBeenCalledWith(7)
    expect(stepUpRun).not.toHaveBeenCalled()
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({
      ok: true, accounts: expect.arrayContaining([expect.objectContaining({ id: 2 })]),
    }), '*')
    wrapper.unmount()
  })

  it('诊断失败仍向插件交付每个账号的详细状态', async () => {
    const result = { success: false, message: 'Login required', latency_ms: 1, status_json: '{"accounts":[{"account_id":2,"status":"login_required"}]}' }
    testPlugin.mockResolvedValue(result)
    const { wrapper, frame, postMessage } = await openPluginUI()
    sendBridgeMessage(frame, 'config.test')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({ ok: true, result }), '*')
    wrapper.unmount()
  })

  it('账号初始化超过普通桥接等待时间后仍交付结果', async () => {
    const { wrapper, frame, postMessage } = await openPluginUI()
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    let complete!: (value: unknown) => void
    testPlugin.mockImplementation(() => new Promise((resolve) => { complete = resolve }))
    const result = { success: true, message: 'OAuth ready', latency_ms: 60_000 }
    try {
      sendBridgeMessage(frame, 'config.test')
      await flushPromises()
      await vi.advanceTimersByTimeAsync(60_000)
      complete(result)
      await flushPromises()
      expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({ ok: true, result }), '*')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })
})
