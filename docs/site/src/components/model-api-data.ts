import type { TOCItemType } from 'fumadocs-core/toc';

export type HttpMethod = 'GET' | 'POST';
export type CodeLanguage = 'curl' | 'javascript' | 'go' | 'python' | 'java' | 'csharp';
export type Protocol = 'openai' | 'responses' | 'anthropic' | 'gemini';
export type EndpointId =
  | 'openai-chat-completions'
  | 'openai-responses'
  | 'claude-messages'
  | 'gemini-generate-content'
  | 'openai-image-generations'
  | 'openai-image-edits'
  | 'openai-list-models'
  | 'gemini-list-models'
  | 'usage'
  | 'antigravity-messages'
  | 'antigravity-gemini-generate-content';

export type FieldInfo = {
  description: string;
  name: string;
  required?: boolean;
  type: string;
  children?: FieldInfo[];
};

export type ErrorSample = {
  status: string;
  label: string;
  sample: unknown;
};

export type EndpointConfig = {
  authScheme?: string;
  baseUrl: string;
  bodyFormat: 'application/json' | 'multipart/form-data' | 'none';
  category: string;
  description: string;
  format: string;
  method: HttpMethod;
  path: string;
  protocol: Protocol;
  requestFields: FieldInfo[];
  requestSample?: unknown;
  responseFields: FieldInfo[];
  responseSample: unknown;
  summary: string;
  title: string;
  /** 覆盖某个状态码的错误示例（按协议默认生成之外的特例，如图像端点的 404）。 */
  errorOverrides?: Partial<Record<string, unknown>>;
  /** 额外补充的错误状态码（如 403 余额不足、404 不支持）。 */
  extraErrorStatuses?: string[];
};

export const languageLabels: Record<CodeLanguage, string> = {
  curl: 'cURL',
  javascript: 'JavaScript',
  go: 'Go',
  python: 'Python',
  java: 'Java',
  csharp: 'C#',
};

export const endpoints: Record<EndpointId, EndpointConfig> = {
  'openai-chat-completions': {
    title: 'ChatCompletions 格式',
    category: '聊天（Chat）',
    format: '原生 OpenAI 格式',
    method: 'POST',
    protocol: 'openai',
    baseUrl: 'https://ai-pixel.online',
    path: '/v1/chat/completions',
    bodyFormat: 'application/json',
    summary: '根据 messages 对话历史创建模型回复，适合 OpenAI SDK、Cherry Studio、沉浸式翻译、通用聊天客户端和脚本调用。',
    description: 'Pixel API 会根据 API Key 绑定的分组自动选择真实上游账号。OpenAI 分组直连 OpenAI 兼容链路，非 OpenAI 分组会转换为对应平台格式。',
    extraErrorStatuses: ['403'],
    requestFields: [
      { name: 'model', type: 'string', required: true, description: '模型名称。必须属于当前 API Key 可访问分组支持的模型。' },
      {
        name: 'messages',
        type: 'array<object>',
        required: true,
        description: '对话消息数组，常见 role 包括 system、user、assistant、tool。',
        children: [
          { name: 'role', type: 'string', required: true, description: '消息角色，例如 system、user、assistant、tool。' },
          { name: 'content', type: 'string | array<object>', required: true, description: '消息内容。可为纯文本，也可为包含 text/image 的多模态数组。' },
          { name: 'name', type: 'string', description: '可选的发送者名称，常用于区分同一 role 下的多个参与者。' },
          { name: 'tool_call_id', type: 'string', description: 'role 为 tool 时，对应的工具调用 ID。' },
        ],
      },
      { name: 'stream', type: 'boolean', description: '是否启用 SSE 流式响应。客户端支持流式输出时可设为 true。' },
      {
        name: 'stream_options',
        type: 'object',
        description: '流式响应的附加选项，仅在 stream=true 时生效。',
        children: [
          { name: 'include_usage', type: 'boolean', description: '是否在流结束时附带一条 usage 统计块。' },
        ],
      },
      { name: 'temperature', type: 'number', description: '采样温度。是否生效取决于真实上游和模型能力。' },
      { name: 'max_tokens', type: 'integer', description: '最大输出 Token。不同模型可能使用 max_completion_tokens 等兼容字段。' },
      {
        name: 'tools',
        type: 'array<object>',
        description: '工具调用定义。适合函数调用、代码工具或客户端插件场景。',
        children: [
          { name: 'type', type: 'string', required: true, description: '工具类型，通常为 function。' },
          {
            name: 'function',
            type: 'object',
            required: true,
            description: '函数定义。',
            children: [
              { name: 'name', type: 'string', required: true, description: '函数名。' },
              { name: 'description', type: 'string', description: '函数用途说明，供模型判断何时调用。' },
              { name: 'parameters', type: 'object', description: 'JSON Schema 形式的参数定义。' },
            ],
          },
        ],
      },
      { name: 'tool_choice', type: 'string | object', description: '控制是否强制调用工具。' },
    ],
    responseFields: [
      { name: 'id', type: 'string', description: '响应 ID。' },
      { name: 'object', type: 'string', description: '通常为 chat.completion 或 chat.completion.chunk。' },
      {
        name: 'choices',
        type: 'array<object>',
        description: '模型输出结果。非流式通常读取 choices[0].message.content。',
        children: [
          { name: 'index', type: 'integer', description: '结果索引。' },
          { name: 'message', type: 'object', description: '包含 role 与 content 的回复消息。' },
          { name: 'finish_reason', type: 'string', description: '结束原因，例如 stop、length、tool_calls。' },
        ],
      },
      { name: 'usage', type: 'object', description: 'Token 消耗统计。' },
    ],
    requestSample: {
      model: 'gpt-5.5',
      messages: [{ role: 'user', content: '你好，请只回复：Pixel API 已连接' }],
      stream: false,
    },
    responseSample: {
      id: 'chatcmpl_example',
      object: 'chat.completion',
      created: 0,
      model: 'gpt-5.5',
      choices: [
        {
          index: 0,
          message: { role: 'assistant', content: 'Pixel API 已连接' },
          finish_reason: 'stop',
        },
      ],
      usage: { prompt_tokens: 12, completion_tokens: 6, total_tokens: 18 },
    },
  },
  'openai-responses': {
    title: 'Responses 格式',
    category: '聊天（Chat）',
    format: '原生 OpenAI 格式',
    method: 'POST',
    protocol: 'responses',
    baseUrl: 'https://ai-pixel.online',
    path: '/v1/responses',
    bodyFormat: 'application/json',
    summary: '创建 Responses 响应，适合 Codex、工具调用、多轮任务、结构化输出和新版 OpenAI 客户端。',
    description: '该接口同时支持 /responses、/responses/*subpath 和 /backend-api/codex/responses 兼容入口。常规客户端优先使用 /v1/responses。',
    requestFields: [
      { name: 'model', type: 'string', required: true, description: '模型名称。建议先调用模型列表确认可用模型。' },
      {
        name: 'input',
        type: 'string | array<object>',
        required: true,
        description: '模型输入。可以是简单字符串，也可以是包含 role/content 的结构化输入。',
        children: [
          { name: 'role', type: 'string', description: '输入项角色，例如 user、assistant、system。' },
          { name: 'content', type: 'string | array<object>', description: '输入内容，可为文本或多模态内容块。' },
        ],
      },
      { name: 'instructions', type: 'string', description: '开发者指令或系统提示。' },
      { name: 'tools', type: 'array<object>', description: '工具定义。' },
      { name: 'stream', type: 'boolean', description: '是否启用流式响应。' },
      { name: 'previous_response_id', type: 'string', description: '多轮响应续接时使用。' },
      { name: 'max_output_tokens', type: 'integer', description: '最大输出 Token。' },
    ],
    responseFields: [
      { name: 'id', type: 'string', description: 'Response ID。' },
      { name: 'status', type: 'string', description: '响应状态，例如 completed。' },
      { name: 'output', type: 'array<object>', description: '结构化输出数组。' },
      { name: 'output_text', type: 'string', description: '便于直接读取的文本输出。' },
      { name: 'usage', type: 'object', description: 'Token 使用统计。' },
    ],
    requestSample: {
      model: 'gpt-5.5',
      input: '你好，请只回复：Pixel API Responses 已连接',
      stream: false,
    },
    responseSample: {
      id: 'resp_example',
      object: 'response',
      status: 'completed',
      model: 'gpt-5.5',
      output_text: 'Pixel API Responses 已连接',
      output: [
        {
          type: 'message',
          role: 'assistant',
          content: [{ type: 'output_text', text: 'Pixel API Responses 已连接' }],
        },
      ],
      usage: { input_tokens: 12, output_tokens: 7, total_tokens: 19 },
    },
  },
  'claude-messages': {
    title: '原生 Claude 格式',
    category: '聊天（Chat）',
    format: 'Anthropic Messages',
    method: 'POST',
    protocol: 'anthropic',
    baseUrl: 'https://ai-pixel.online',
    path: '/v1/messages',
    bodyFormat: 'application/json',
    summary: '按 Anthropic Messages 格式创建回复，适合 Claude Code、Claude SDK、Claude 风格客户端和需要 max_tokens 的工具。',
    description: 'OpenAI 分组请求该入口时会进入 OpenAI 兼容转换链路；Anthropic、Bedrock、Vertex 等 Claude 分组会按平台能力转发。',
    extraErrorStatuses: ['404'],
    requestFields: [
      { name: 'model', type: 'string', required: true, description: 'Claude 格式模型名或已映射的模型名。' },
      {
        name: 'messages',
        type: 'array<object>',
        required: true,
        description: 'Anthropic messages 数组。',
        children: [
          { name: 'role', type: 'string', required: true, description: '消息角色，通常为 user 或 assistant。' },
          { name: 'content', type: 'string | array<object>', required: true, description: '消息内容，可为文本或内容块数组。' },
        ],
      },
      { name: 'max_tokens', type: 'integer', required: true, description: '最大输出 Token，Anthropic 格式通常必须传入。' },
      {
        name: 'system',
        type: 'string | array<object>',
        description: '系统提示词。',
        children: [
          { name: 'type', type: 'string', description: '内容块类型，例如 text。' },
          { name: 'text', type: 'string', description: '系统提示文本。' },
        ],
      },
      { name: 'stream', type: 'boolean', description: '是否启用流式响应。' },
      { name: 'tools', type: 'array<object>', description: 'Claude 工具定义。' },
    ],
    responseFields: [
      { name: 'id', type: 'string', description: '消息 ID。' },
      { name: 'type', type: 'string', description: '通常为 message。' },
      { name: 'role', type: 'string', description: '通常为 assistant。' },
      { name: 'content', type: 'array<object>', description: '回复内容块。' },
      { name: 'usage', type: 'object', description: '输入和输出 Token 统计。' },
    ],
    requestSample: {
      model: 'claude-3-5-sonnet-latest',
      max_tokens: 256,
      messages: [{ role: 'user', content: 'Hello' }],
    },
    responseSample: {
      id: 'msg_example',
      type: 'message',
      role: 'assistant',
      model: 'claude-3-5-sonnet-latest',
      content: [{ type: 'text', text: 'Hello!' }],
      stop_reason: 'end_turn',
      usage: { input_tokens: 8, output_tokens: 4 },
    },
  },
  'gemini-generate-content': {
    title: 'Gemini 文本聊天',
    category: '聊天（Chat）',
    format: '原生 Gemini 格式',
    method: 'POST',
    protocol: 'gemini',
    baseUrl: 'https://ai-pixel.online',
    path: '/v1beta/models/{model}:generateContent',
    bodyFormat: 'application/json',
    summary: '按 Gemini v1beta generateContent 格式创建文本或多模态回复，适合 Gemini SDK、Gemini CLI 和原生 Gemini 客户端。',
    description: 'Pixel API 使用 Bearer API Key 鉴权；如果客户端原本使用 key 查询参数，建议改为 Authorization 请求头。',
    requestFields: [
      { name: 'model', type: 'path string', required: true, description: '路径中的 Gemini 模型名，例如 gemini-2.5-pro。' },
      {
        name: 'contents',
        type: 'array<object>',
        required: true,
        description: 'Gemini contents 数组，包含 role 和 parts。',
        children: [
          { name: 'role', type: 'string', description: '内容角色，例如 user、model。' },
          { name: 'parts', type: 'array<object>', required: true, description: '内容片段数组，常见字段为 text 或 inlineData。' },
        ],
      },
      {
        name: 'generationConfig',
        type: 'object',
        description: '温度、最大输出、候选数等生成参数。',
        children: [
          { name: 'temperature', type: 'number', description: '采样温度。' },
          { name: 'maxOutputTokens', type: 'integer', description: '最大输出 Token。' },
          { name: 'candidateCount', type: 'integer', description: '候选回复数量。' },
        ],
      },
      { name: 'systemInstruction', type: 'object', description: '系统指令。' },
      { name: 'safetySettings', type: 'array<object>', description: '安全策略设置。' },
      { name: 'tools', type: 'array<object>', description: '工具定义。' },
    ],
    responseFields: [
      { name: 'candidates', type: 'array<object>', description: '候选回复。文本通常位于 candidates[0].content.parts[0].text。' },
      { name: 'usageMetadata', type: 'object', description: 'Token 使用统计。' },
      { name: 'modelVersion', type: 'string', description: '模型版本。' },
    ],
    requestSample: {
      contents: [
        {
          role: 'user',
          parts: [{ text: '你好，请只回复：Pixel API Gemini 已连接' }],
        },
      ],
      generationConfig: { temperature: 0.7 },
    },
    responseSample: {
      candidates: [
        {
          content: {
            role: 'model',
            parts: [{ text: 'Pixel API Gemini 已连接' }],
          },
          finishReason: 'STOP',
        },
      ],
      usageMetadata: { promptTokenCount: 12, candidatesTokenCount: 7, totalTokenCount: 19 },
      modelVersion: 'gemini-2.5-pro',
    },
  },
  'openai-image-generations': {
    title: '生成图像',
    category: '图像（Images）',
    format: '原生 OpenAI 格式',
    method: 'POST',
    protocol: 'openai',
    baseUrl: 'https://ai-pixel.online',
    path: '/v1/images/generations',
    bodyFormat: 'application/json',
    summary: '根据提示词生成图片，仅 OpenAI 平台分组支持。',
    description: '如果 API Key 绑定的不是 OpenAI 平台分组，接口会返回 not_found_error。',
    extraErrorStatuses: ['404'],
    errorOverrides: {
      '404': {
        error: {
          type: 'not_found_error',
          message: 'Images API is not supported for this platform',
        },
      },
    },
    requestFields: [
      { name: 'model', type: 'string', required: true, description: '图片模型名。' },
      { name: 'prompt', type: 'string', required: true, description: '图片生成提示词。' },
      { name: 'size', type: 'string', description: '图片尺寸，例如 1024x1024。' },
      { name: 'n', type: 'integer', description: '生成图片数量。' },
      { name: 'response_format', type: 'string', description: '返回 URL 或 b64_json，取决于上游支持。' },
    ],
    responseFields: [
      { name: 'created', type: 'integer', description: '创建时间戳。' },
      { name: 'data', type: 'array<object>', description: '图片结果。常见字段为 url 或 b64_json。' },
    ],
    requestSample: {
      model: 'gpt-image-1',
      prompt: '一张极简风格的 Pixel API 标志海报',
      size: '1024x1024',
      n: 1,
    },
    responseSample: {
      created: 0,
      data: [{ url: 'https://example.com/generated-image.png' }],
    },
  },
  'openai-image-edits': {
    title: '编辑图像',
    category: '图像（Images）',
    format: '原生 OpenAI 格式',
    method: 'POST',
    protocol: 'openai',
    baseUrl: 'https://ai-pixel.online',
    path: '/v1/images/edits',
    bodyFormat: 'multipart/form-data',
    summary: '基于输入图片和提示词编辑图片，仅 OpenAI 平台分组支持。',
    description: '该接口使用 multipart/form-data 上传图片文件。非 OpenAI 平台分组会返回 not_found_error。',
    extraErrorStatuses: ['404'],
    errorOverrides: {
      '404': {
        error: {
          type: 'not_found_error',
          message: 'Images API is not supported for this platform',
        },
      },
    },
    requestFields: [
      { name: 'model', type: 'string', required: true, description: '图片编辑模型名。' },
      { name: 'image', type: 'file', required: true, description: '待编辑图片文件。' },
      { name: 'prompt', type: 'string', required: true, description: '编辑说明。' },
      { name: 'mask', type: 'file', description: '可选遮罩图片。' },
      { name: 'size', type: 'string', description: '输出尺寸。' },
      { name: 'n', type: 'integer', description: '生成数量。' },
    ],
    responseFields: [
      { name: 'created', type: 'integer', description: '创建时间戳。' },
      { name: 'data', type: 'array<object>', description: '编辑后的图片结果。' },
    ],
    requestSample: {
      model: 'gpt-image-1',
      image: '@/path/to/image.png',
      prompt: '把背景改成浅色科技风',
      size: '1024x1024',
    },
    responseSample: {
      created: 0,
      data: [{ url: 'https://example.com/edited-image.png' }],
    },
  },
  'openai-list-models': {
    title: '列出模型',
    category: '模型（Models）',
    format: '原生 OpenAI 格式',
    method: 'GET',
    protocol: 'openai',
    baseUrl: 'https://ai-pixel.online',
    path: '/v1/models',
    bodyFormat: 'none',
    summary: '列出当前 API Key 可访问的 OpenAI/Claude 兼容模型。',
    description: '排查模型名错误时，先调用这个接口，以返回列表为准。',
    requestFields: [],
    responseFields: [
      { name: 'object', type: 'string', description: '通常为 list。' },
      { name: 'data', type: 'array<object>', description: '模型列表。' },
    ],
    responseSample: {
      object: 'list',
      data: [{ id: 'gpt-5.5', object: 'model', owned_by: 'pixel-api' }],
    },
  },
  'gemini-list-models': {
    title: '列出 Gemini 模型',
    category: '模型（Models）',
    format: '原生 Gemini 格式',
    method: 'GET',
    protocol: 'gemini',
    baseUrl: 'https://ai-pixel.online',
    path: '/v1beta/models',
    bodyFormat: 'none',
    summary: '列出当前 API Key 可访问的 Gemini v1beta 模型。',
    description: '适合 Gemini SDK/CLI 在启动时读取模型能力。',
    requestFields: [],
    responseFields: [
      { name: 'models', type: 'array<object>', description: 'Gemini 模型列表。' },
    ],
    responseSample: {
      models: [
        {
          name: 'models/gemini-2.5-pro',
          displayName: 'Gemini 2.5 Pro',
          supportedGenerationMethods: ['generateContent'],
        },
      ],
    },
  },
  usage: {
    title: '查询用量',
    category: '模型（Models）',
    format: 'Pixel API 网关格式',
    method: 'GET',
    protocol: 'openai',
    baseUrl: 'https://ai-pixel.online',
    path: '/v1/usage',
    bodyFormat: 'none',
    summary: '查询当前 API Key 的模型调用用量。',
    description: '用于客户端或脚本确认调用是否进入 Pixel API，以及辅助排查费用和请求统计。',
    requestFields: [],
    responseFields: [
      { name: 'total_requests', type: 'integer', description: '请求总数。' },
      { name: 'total_tokens', type: 'integer', description: 'Token 总量。' },
      { name: 'total_cost', type: 'number', description: '消耗金额或额度。' },
    ],
    responseSample: {
      total_requests: 12,
      total_tokens: 34567,
      total_cost: 1.23,
    },
  },
  'antigravity-messages': {
    title: 'Antigravity Messages',
    category: 'Antigravity',
    format: 'Claude 兼容格式',
    method: 'POST',
    protocol: 'anthropic',
    baseUrl: 'https://ai-pixel.online',
    path: '/antigravity/v1/messages',
    bodyFormat: 'application/json',
    summary: '强制使用 Antigravity 账号的 Claude Messages 兼容入口。',
    description: '该入口不与普通分组混合调度，适合明确需要 Antigravity 账号能力的客户端。',
    requestFields: [
      { name: 'model', type: 'string', required: true, description: 'Antigravity 可用模型名。' },
      {
        name: 'messages',
        type: 'array<object>',
        required: true,
        description: 'Anthropic messages 数组。',
        children: [
          { name: 'role', type: 'string', required: true, description: '消息角色，通常为 user 或 assistant。' },
          { name: 'content', type: 'string | array<object>', required: true, description: '消息内容。' },
        ],
      },
      { name: 'max_tokens', type: 'integer', required: true, description: '最大输出 Token。' },
    ],
    responseFields: [
      { name: 'id', type: 'string', description: '消息 ID。' },
      { name: 'content', type: 'array<object>', description: '回复内容。' },
      { name: 'usage', type: 'object', description: 'Token 使用统计。' },
    ],
    requestSample: {
      model: 'claude-sonnet-4-5',
      max_tokens: 256,
      messages: [{ role: 'user', content: 'Hello from Antigravity' }],
    },
    responseSample: {
      id: 'msg_antigravity_example',
      type: 'message',
      role: 'assistant',
      content: [{ type: 'text', text: 'Hello from Antigravity' }],
      usage: { input_tokens: 8, output_tokens: 5 },
    },
  },
  'antigravity-gemini-generate-content': {
    title: 'Antigravity Gemini',
    category: 'Antigravity',
    format: 'Gemini v1beta 格式',
    method: 'POST',
    protocol: 'gemini',
    baseUrl: 'https://ai-pixel.online',
    path: '/antigravity/v1beta/models/{model}:generateContent',
    bodyFormat: 'application/json',
    summary: '强制使用 Antigravity 账号的 Gemini generateContent 入口。',
    description: '该入口使用 Antigravity 平台账号，不与普通 Gemini 分组混合调度。',
    requestFields: [
      { name: 'model', type: 'path string', required: true, description: '路径中的模型名。' },
      {
        name: 'contents',
        type: 'array<object>',
        required: true,
        description: 'Gemini contents 数组。',
        children: [
          { name: 'role', type: 'string', description: '内容角色，例如 user、model。' },
          { name: 'parts', type: 'array<object>', required: true, description: '内容片段数组。' },
        ],
      },
    ],
    responseFields: [
      { name: 'candidates', type: 'array<object>', description: '候选回复。' },
      { name: 'usageMetadata', type: 'object', description: 'Token 使用统计。' },
    ],
    requestSample: {
      contents: [{ role: 'user', parts: [{ text: 'Hello Antigravity Gemini' }] }],
    },
    responseSample: {
      candidates: [{ content: { role: 'model', parts: [{ text: 'Hello Antigravity Gemini' }] } }],
      usageMetadata: { totalTokenCount: 14 },
    },
  },
};

export function formatJson(value: unknown): string {
  return JSON.stringify(value, null, 2);
}

export function concretePath(path: string): string {
  return path.replace('{model}', 'gemini-2.5-pro');
}

export function joinUrl(baseUrl: string, path: string): string {
  return `${baseUrl.replace(/\/+$/, '')}${concretePath(path)}`;
}

function buildMultipartCurl(url: string): string {
  return `curl -X POST "${url}" \\
  -H "Authorization: Bearer sk-你的密钥" \\
  -F "model=gpt-image-1" \\
  -F "image=@/path/to/image.png" \\
  -F "prompt=把背景改成浅色科技风"`;
}

export function buildCode(
  language: CodeLanguage,
  endpoint: EndpointConfig,
  baseUrl: string,
  requestBodyText?: string,
): string {
  const url = joinUrl(baseUrl, endpoint.path);
  const body = requestBodyText ?? (endpoint.requestSample ? formatJson(endpoint.requestSample) : '');

  if (endpoint.bodyFormat === 'multipart/form-data') {
    if (language === 'curl') return buildMultipartCurl(url);
    if (language === 'javascript') {
      return `const form = new FormData();
form.append('model', 'gpt-image-1');
form.append('image', imageFile);
form.append('prompt', '把背景改成浅色科技风');

const response = await fetch('${url}', {
  method: 'POST',
  headers: { Authorization: 'Bearer sk-你的密钥' },
  body: form,
});

console.log(await response.json());`;
    }
    if (language === 'python') {
      return `import requests

with open('/path/to/image.png', 'rb') as image:
    response = requests.post(
        '${url}',
        headers={'Authorization': 'Bearer sk-你的密钥'},
        data={'model': 'gpt-image-1', 'prompt': '把背景改成浅色科技风'},
        files={'image': image},
        timeout=60,
    )

print(response.json())`;
    }
    return `// 使用 multipart/form-data 上传 image 文件到 ${url}`;
  }

  if (language === 'curl') {
    if (endpoint.method === 'GET') {
      return `curl "${url}" \\
  -H "Authorization: Bearer sk-你的密钥"`;
    }

    return `curl -X POST "${url}" \\
  -H "Authorization: Bearer sk-你的密钥" \\
  -H "Content-Type: application/json" \\
  -d '${body.replace(/'/g, `'\"'\"'`)}'`;
  }

  if (language === 'javascript') {
    return `const response = await fetch('${url}', {
  method: '${endpoint.method}',
  headers: {
    Authorization: 'Bearer sk-你的密钥',${
      endpoint.method === 'POST' ? "\n    'Content-Type': 'application/json'," : ''
    }
  },${endpoint.method === 'POST' ? `\n  body: JSON.stringify(${body}),` : ''}
});

console.log(await response.json());`;
  }

  if (language === 'python') {
    if (endpoint.method === 'GET') {
      return `import requests

response = requests.get(
    '${url}',
    headers={'Authorization': 'Bearer sk-你的密钥'},
    timeout=30,
)

print(response.json())`;
    }

    return `import requests

response = requests.post(
    '${url}',
    headers={
        'Authorization': 'Bearer sk-你的密钥',
        'Content-Type': 'application/json',
    },
    json=${body},
    timeout=60,
)

print(response.json())`;
  }

  if (language === 'go') {
    return `package main

import (
  "bytes"
  "fmt"
  "io"
  "net/http"
)

func main() {
  body := []byte(\`${body || '{}'}\`)
  req, _ := http.NewRequest("${endpoint.method}", "${url}", bytes.NewReader(body))
  req.Header.Set("Authorization", "Bearer sk-你的密钥")
  req.Header.Set("Content-Type", "application/json")

  resp, _ := http.DefaultClient.Do(req)
  defer resp.Body.Close()
  data, _ := io.ReadAll(resp.Body)
  fmt.Println(string(data))
}`;
  }

  if (language === 'java') {
    return `HttpRequest request = HttpRequest.newBuilder()
    .uri(URI.create("${url}"))
    .header("Authorization", "Bearer sk-你的密钥")
    .header("Content-Type", "application/json")
    .method("${endpoint.method}", ${
      endpoint.method === 'GET' ? 'HttpRequest.BodyPublishers.noBody()' : `HttpRequest.BodyPublishers.ofString("""\n${body}\n""")`
    })
    .build();

HttpResponse<String> response = HttpClient.newHttpClient()
    .send(request, HttpResponse.BodyHandlers.ofString());

System.out.println(response.body());`;
  }

  return `using var client = new HttpClient();
client.DefaultRequestHeaders.Authorization =
    new AuthenticationHeaderValue("Bearer", "sk-你的密钥");

var response = await client.SendAsync(new HttpRequestMessage(
    HttpMethod.${endpoint.method === 'GET' ? 'Get' : 'Post'},
    "${url}")${
      endpoint.method === 'POST'
        ? ` {
    Content = new StringContent("""
${body}
""", Encoding.UTF8, "application/json")
}`
        : ''
    });

Console.WriteLine(await response.Content.ReadAsStringAsync());`;
}

/** 401 中间件返回的是扁平结构，所有协议一致。 */
const AUTH_401_SAMPLE = { code: 'INVALID_API_KEY', message: 'Invalid API key' };

type ErrorBlueprint = { message: string; type: string; googleStatus: string };

const ERROR_BLUEPRINTS: Record<string, ErrorBlueprint> = {
  '400': { type: 'invalid_request_error', message: 'model is required', googleStatus: 'INVALID_ARGUMENT' },
  '403': { type: 'billing_error', message: 'insufficient balance', googleStatus: 'PERMISSION_DENIED' },
  '404': { type: 'not_found_error', message: 'not supported for this platform', googleStatus: 'NOT_FOUND' },
  '429': { type: 'rate_limit_error', message: 'Too many pending requests, please retry later', googleStatus: 'RESOURCE_EXHAUSTED' },
};

function shapeError(protocol: Protocol, status: string, blueprint: ErrorBlueprint): unknown {
  switch (protocol) {
    case 'responses':
      return { error: { code: blueprint.type, message: blueprint.message } };
    case 'anthropic':
      return { type: 'error', error: { type: blueprint.type, message: blueprint.message } };
    case 'gemini':
      return { error: { code: Number(status), message: blueprint.message, status: blueprint.googleStatus } };
    case 'openai':
    default:
      return { error: { type: blueprint.type, message: blueprint.message } };
  }
}

/** 生成响应示例的状态码列表：200 成功 + 常见错误码，按协议给出真实错误形状。 */
export function buildErrorSamples(config: EndpointConfig): ErrorSample[] {
  const statuses = ['400', '401', '429', ...(config.extraErrorStatuses ?? [])];
  const seen = new Set<string>();
  const errorSamples: ErrorSample[] = [];

  for (const status of statuses) {
    if (seen.has(status)) continue;
    seen.add(status);
    const override = config.errorOverrides?.[status];
    if (override) {
      errorSamples.push({ status, label: status, sample: override });
      continue;
    }
    if (status === '401') {
      // 鉴权在网关中间件即被拒绝，早于协议转换，所有协议都返回同一扁平结构。
      errorSamples.push({ status, label: status, sample: AUTH_401_SAMPLE });
      continue;
    }
    const blueprint = ERROR_BLUEPRINTS[status];
    if (!blueprint) continue;
    errorSamples.push({
      status,
      label: status,
      sample: shapeError(config.protocol, status, blueprint),
    });
  }

  return [
    { status: '200', label: '200', sample: config.responseSample },
    ...errorSamples,
  ];
}

/** 供服务端渲染时构造「本页目录」，锚点与组件渲染的 id 一一对应。 */
export function buildEndpointToc(id: EndpointId): TOCItemType[] {
  const config = endpoints[id];
  if (!config) return [];

  const bodyTitle = config.bodyFormat === 'none' ? 'Query' : 'Request Body';

  return [
    { title: 'Authorization', url: '#authorization', depth: 2 },
    { title: bodyTitle, url: '#request-body', depth: 2 },
    { title: 'Response Body', url: '#response-body', depth: 2 },
    { title: '示例代码', url: '#code-samples', depth: 2 },
    { title: '说明', url: '#description', depth: 2 },
  ];
}
