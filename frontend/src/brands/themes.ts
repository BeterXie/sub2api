export const themes: Record<string, { accent: string; headline: string; description: string }> = {
  llmp: { accent: '#0f766e', headline: '连接模型，构建应用。', description: '在自己的工作流中使用模型 API。' },
  mues: { accent: '#a84320', headline: '让想法进入工作流。', description: '从一次模型调用，开始你的下一步创作。' },
  aisi: { accent: '#1559a2', headline: '从接口，到你的应用。', description: '管理密钥、模型与用量，让开发过程更清晰。' },
  opensi_codes: { accent: '#237545', headline: '把模型接入代码。', description: '熟悉的 API 格式，面向开发的接入体验。' },
  opensi_in: { accent: '#6042a0', headline: '为日常工作接入 AI。', description: '按需选择模型，在一个控制台管理你的调用。' }
}

export const englishThemes: Record<string, { headline: string; description: string }> = {
  llmp: { headline: 'Connect models. Build applications.', description: 'Use model APIs in your own workflow.' },
  mues: { headline: 'Bring your ideas into the workflow.', description: 'Start your next creation with a model call.' },
  aisi: { headline: 'From API to your application.', description: 'Manage keys, models and usage in one console.' },
  opensi_codes: { headline: 'Connect models to your code.', description: 'Familiar API formats for your next integration.' },
  opensi_in: { headline: 'Bring AI into everyday work.', description: 'Choose models as you need them and manage your calls.' }
}
