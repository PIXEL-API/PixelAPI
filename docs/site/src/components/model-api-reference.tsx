'use client';

import { type CSSProperties, useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { DynamicCodeBlock } from 'fumadocs-ui/components/dynamic-codeblock';
import {
  type CodeLanguage,
  type EndpointId,
  type FieldInfo,
  buildCode,
  buildErrorSamples,
  concretePath,
  endpoints,
  formatJson,
  joinUrl,
  languageLabels,
} from '@/components/model-api-data';

type CopyTarget = 'markdown' | 'code' | 'response';
type CopyErrorTarget = CopyTarget | null;
type FieldPopoverPosition = {
  left: number;
  maxHeight: number;
  top: number;
  width: number;
};
type JsonObject = Record<string, unknown>;
type LiveResponseView = 'pretty' | 'raw';
type LiveResponse = {
  bodyText: string;
  durationMs: number;
  formattedBody: string;
  ok: boolean;
  parsedBody?: unknown;
  previewLabel: string;
  previewText: string;
  requestUrl: string;
  sizeBytes: number;
  status: string;
  statusText: string;
  suggestion: string;
};

function isRecord(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function parseJsonObject(value: string): JsonObject {
  if (!value.trim()) return {};
  const parsed = JSON.parse(value) as unknown;
  return isRecord(parsed) ? parsed : {};
}

function tryParseJsonObject(value: string): JsonObject {
  try {
    return parseJsonObject(value);
  } catch {
    return {};
  }
}

function parseMaybeJson(value: string): unknown {
  if (!value.trim()) return undefined;
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return undefined;
  }
}

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

function truncatePreview(value: string): string {
  const normalized = value.replace(/\s+/g, ' ').trim();
  return normalized.length > 280 ? `${normalized.slice(0, 280)}...` : normalized;
}

function firstString(...values: unknown[]): string {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value;
  }

  return '';
}

function findTextValue(value: unknown, depth = 0): string {
  if (depth > 5) return '';
  if (typeof value === 'string') return value;
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = findTextValue(item, depth + 1);
      if (found) return found;
    }
    return '';
  }
  if (!isRecord(value)) return '';

  const direct = firstString(value.output_text, value.text);
  if (direct) return direct;

  for (const key of ['content', 'message', 'delta', 'output', 'parts', 'candidates']) {
    const found = findTextValue(value[key], depth + 1);
    if (found) return found;
  }

  return '';
}

function extractResponsePreview(parsedBody: unknown, bodyText: string): { label: string; text: string } {
  if (isRecord(parsedBody)) {
    if (isRecord(parsedBody.error)) {
      return {
        label: '错误信息',
        text: truncatePreview(firstString(parsedBody.error.message, parsedBody.error.type, bodyText)),
      };
    }

    const choices = parsedBody.choices;
    if (Array.isArray(choices) && isRecord(choices[0])) {
      const choice = choices[0];
      const message = isRecord(choice.message) ? choice.message : undefined;
      const delta = isRecord(choice.delta) ? choice.delta : undefined;
      const content = firstString(message?.content, delta?.content, choice.text);
      if (content) {
        return {
          label: 'Assistant message',
          text: truncatePreview(content),
        };
      }
    }

    const outputText = firstString(parsedBody.output_text);
    if (outputText) {
      return {
        label: 'Output text',
        text: truncatePreview(outputText),
      };
    }

    const data = parsedBody.data;
    if (Array.isArray(data) && isRecord(data[0])) {
      const imageUrl = firstString(data[0].url);
      if (imageUrl) {
        return {
          label: 'Image URL',
          text: truncatePreview(imageUrl),
        };
      }
      if (typeof data[0].b64_json === 'string') {
        return {
          label: 'Image data',
          text: '已收到 Base64 图片数据，可在完整 JSON 中复制或解析。',
        };
      }
    }

    const text = findTextValue(parsedBody);
    if (text) {
      return {
        label: 'Response preview',
        text: truncatePreview(text),
      };
    }
  }

  return {
    label: 'Response preview',
    text: truncatePreview(bodyText || '响应体为空。'),
  };
}

function responseSuggestion(status: string, ok: boolean, parsedBody: unknown): string {
  const message = isRecord(parsedBody) && isRecord(parsedBody.error) ? firstString(parsedBody.error.message) : '';

  if (ok) return '请求成功，下面是服务端返回内容。';
  if (status === 'local') return '请求还没有发出，请先补齐必要信息。';
  if (status === 'network') return '浏览器未能完成请求，请检查 Base URL、网络状态或跨域限制。';
  if (status === '400') return '请求参数有误，请检查 Body JSON、字段类型和必填参数。';
  if (status === '401') return '认证失败，请检查 Authorization 是否填写了有效的 Bearer Token。';
  if (status === '403') {
    return message.toLowerCase().includes('balance')
      ? '账户余额不足，请充值后重试。'
      : '当前 API Key 没有访问权限，或账号余额/分组权限不满足该模型。';
  }
  if (status === '404') return '请求路径、模型或上游能力不存在，请确认 Base URL、Path 和 model。';
  if (status === '429') return '请求频率过高，请稍后重试或降低并发。';
  if (/^5/.test(status)) return '服务端或上游模型返回异常，请稍后重试；若持续出现，请复制响应详情排查。';

  return '请求没有按预期成功，建议先查看完整响应 JSON。';
}

function createLocalResponse(message: string, requestUrl: string): LiveResponse {
  const parsedBody = {
    error: {
      type: 'validation_error',
      message,
    },
  };
  const formattedBody = formatJson(parsedBody);

  return {
    bodyText: formattedBody,
    durationMs: 0,
    formattedBody,
    ok: false,
    parsedBody,
    previewLabel: '请求未发送',
    previewText: message,
    requestUrl,
    sizeBytes: new Blob([formattedBody]).size,
    status: 'local',
    statusText: 'Not sent',
    suggestion: responseSuggestion('local', false, parsedBody),
  };
}

function createNetworkErrorResponse(message: string, requestUrl: string, durationMs: number): LiveResponse {
  const parsedBody = {
    error: {
      type: 'network_error',
      message,
    },
  };
  const formattedBody = formatJson(parsedBody);

  return {
    bodyText: formattedBody,
    durationMs,
    formattedBody,
    ok: false,
    parsedBody,
    previewLabel: '请求失败',
    previewText: message,
    requestUrl,
    sizeBytes: new Blob([formattedBody]).size,
    status: 'network',
    statusText: 'Network error',
    suggestion: responseSuggestion('network', false, parsedBody),
  };
}

function stringifyFieldValue(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value === 'object') return formatJson(value);
  return String(value);
}

function isStructuredField(field: FieldInfo): boolean {
  return /\b(object|array)\b/.test(field.type);
}

function fieldTone(field: FieldInfo): string {
  if (field.type.includes('boolean')) return 'boolean';
  if (field.type.includes('integer') || field.type.includes('number')) return 'number';
  if (field.type.includes('array')) return 'array';
  if (field.type.includes('object')) return 'object';
  if (field.type.includes('file')) return 'file';
  return 'string';
}

function fieldPathLabel(path: string, field: FieldInfo): string {
  if (field.type.includes('array') && field.type.includes('object') && !path.endsWith('[]')) {
    return `${path}[]`;
  }

  return path;
}

function fieldDetailTarget(field: FieldInfo, path: string): { fields: FieldInfo[]; path: string } {
  const rootPath = fieldPathLabel(path, field);
  const nestedObject = field.children?.find((child) => Boolean(child.children?.length));

  if (nestedObject?.children?.length) {
    return {
      fields: nestedObject.children,
      path: `${rootPath}.${nestedObject.name}`,
    };
  }

  return {
    fields: field.children ?? [],
    path: rootPath,
  };
}

function parsePrimitiveFieldValue(field: FieldInfo, value: string): unknown {
  if (!value.trim()) return undefined;
  if (field.type.includes('boolean')) return value === 'true';
  if (field.type.includes('integer')) {
    const parsed = Number.parseInt(value, 10);
    return Number.isFinite(parsed) ? parsed : undefined;
  }
  if (field.type.includes('number')) {
    const parsed = Number.parseFloat(value);
    return Number.isFinite(parsed) ? parsed : undefined;
  }
  return value;
}

function placeholderForField(field: FieldInfo): string {
  if (field.type.includes('boolean')) return 'Unset';
  if (field.type.includes('integer') || field.type.includes('number')) return 'Enter value';
  if (field.type.includes('array')) return '[]';
  if (field.type.includes('object')) return '{}';
  if (field.name === 'model') return 'gpt-5.5';
  return 'Enter value';
}

function JsonFieldInput({
  field,
  onCommit,
  showLabel = true,
  value,
}: {
  field: FieldInfo;
  onCommit: (value: unknown) => void;
  showLabel?: boolean;
  value: unknown;
}) {
  const [draft, setDraft] = useState(() => stringifyFieldValue(value));
  const [error, setError] = useState('');

  return (
    <label className="model-api-param-input model-api-param-input-json">
      {showLabel ? <span>{field.name}</span> : null}
      <textarea
        aria-label={showLabel ? undefined : field.name}
        onChange={(event) => {
          const next = event.target.value;
          setDraft(next);

          if (!next.trim()) {
            setError('');
            onCommit(undefined);
            return;
          }

          try {
            onCommit(JSON.parse(next) as unknown);
            setError('');
          } catch {
            setError('JSON 格式未通过校验，暂未写入请求体。');
          }
        }}
        placeholder={placeholderForField(field)}
        rows={field.type.includes('array') ? 6 : 5}
        spellCheck={false}
        value={draft}
      />
      {error ? <small role="status">{error}</small> : null}
    </label>
  );
}

function ClipboardIcon() {
  return (
    <svg aria-hidden="true" fill="none" viewBox="0 0 24 24">
      <path
        d="M8.5 6.5H7.75A2.25 2.25 0 0 0 5.5 8.75v9.5a2.25 2.25 0 0 0 2.25 2.25h8.5a2.25 2.25 0 0 0 2.25-2.25v-9.5A2.25 2.25 0 0 0 16.25 6.5h-.75"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.8"
      />
      <path
        d="M9 5.75A2.25 2.25 0 0 1 11.25 3.5h1.5A2.25 2.25 0 0 1 15 5.75v.5a.75.75 0 0 1-.75.75h-4.5A.75.75 0 0 1 9 6.25v-.5Z"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.8"
      />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg aria-hidden="true" fill="none" viewBox="0 0 24 24">
      <path
        d="m5.75 12.75 4.25 4.25 8.25-9.5"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="2"
      />
    </svg>
  );
}

function PanelCopyButton({
  copied,
  failed,
  label,
  onClick,
}: {
  copied: boolean;
  failed?: boolean;
  label: string;
  onClick: () => void;
}) {
  const stateLabel = failed ? '复制失败' : copied ? '已复制' : label;

  return (
    <button
      aria-label={stateLabel}
      className="model-api-copy-button"
      data-copied={copied ? 'true' : 'false'}
      data-failed={failed ? 'true' : 'false'}
      onClick={onClick}
      title={stateLabel}
      type="button"
    >
      {copied ? <CheckIcon /> : <ClipboardIcon />}
    </button>
  );
}

function BooleanFieldControl({
  field,
  onChange,
  showLabel = true,
  value,
}: {
  field: FieldInfo;
  onChange: (value: unknown) => void;
  showLabel?: boolean;
  value: unknown;
}) {
  const [open, setOpen] = useState(false);
  const current = value === true ? 'true' : value === false ? 'false' : 'unset';
  const controlId = useId();
  const labelId = `${controlId}-label`;
  const listboxId = `${controlId}-options`;
  const options = [
    { label: 'Unset', nextValue: undefined, value: 'unset' },
    { label: 'true', nextValue: true, value: 'true' },
    { label: 'false', nextValue: false, value: 'false' },
  ] as const;
  const activeOption = options.find((option) => option.value === current) ?? options[0];

  return (
    <div
      className="model-api-param-input model-api-param-input-boolean"
      onBlur={(event) => {
        const nextFocus = event.relatedTarget;
        if (!(nextFocus instanceof Node) || !event.currentTarget.contains(nextFocus)) {
          setOpen(false);
        }
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') setOpen(false);
      }}
    >
      {showLabel ? <span id={labelId}>{field.name}</span> : null}
      <button
        aria-label={showLabel ? undefined : field.name}
        aria-labelledby={showLabel ? labelId : undefined}
        aria-controls={listboxId}
        aria-expanded={open}
        aria-haspopup="listbox"
        className="model-api-boolean-select-trigger"
        onClick={() => setOpen((currentOpen) => !currentOpen)}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown' || event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            setOpen(true);
          }
        }}
        type="button"
      >
        <span className="model-api-boolean-current">{activeOption.label}</span>
        <span aria-hidden="true" className="model-api-select-caret" />
      </button>
      {open ? (
        <div className="model-api-boolean-select-menu" id={listboxId} role="listbox">
          {options.map((option) => (
            <button
              aria-selected={current === option.value}
              className="model-api-boolean-select-option"
              key={option.value}
              onClick={() => {
                onChange(option.nextValue);
                setOpen(false);
              }}
              role="option"
              type="button"
            >
              {option.label}
              {current === option.value ? <CheckIcon /> : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function FieldValueInput({
  field,
  onChange,
  showLabel = true,
  value,
}: {
  field: FieldInfo;
  onChange: (value: unknown) => void;
  showLabel?: boolean;
  value: unknown;
}) {
  if (isStructuredField(field)) {
    return (
      <JsonFieldInput
        field={field}
        key={`${field.name}:${stringifyFieldValue(value)}`}
        onCommit={onChange}
        showLabel={showLabel}
        value={value}
      />
    );
  }

  if (field.type.includes('boolean')) {
    return <BooleanFieldControl field={field} onChange={onChange} showLabel={showLabel} value={value} />;
  }

  return (
    <label className="model-api-param-input">
      {showLabel ? <span>{field.name}</span> : null}
      <input
        aria-label={showLabel ? undefined : field.name}
        inputMode={field.type.includes('integer') || field.type.includes('number') ? 'decimal' : undefined}
        onChange={(event) => onChange(parsePrimitiveFieldValue(field, event.target.value))}
        placeholder={placeholderForField(field)}
        spellCheck={false}
        value={stringifyFieldValue(value)}
      />
    </label>
  );
}

function RequestFieldControl({
  field,
  onChange,
  value,
}: {
  field: FieldInfo;
  onChange: (value: unknown) => void;
  value: unknown;
}) {
  const hasChildren = Boolean(field.children?.length);
  const structured = isStructuredField(field);
  const [open, setOpen] = useState(field.required || field.name === 'stream_options');
  const tone = fieldTone(field);

  if (!hasChildren && !structured) {
    return (
      <div className="model-api-param-field" data-tone={tone}>
        <div className="model-api-param-field-head">
          <div className="model-api-param-field-title">
            <code>{field.name}</code>
            {field.required ? <em>required</em> : null}
          </div>
          <span>{field.type}</span>
        </div>
        <p>{field.description}</p>
        <FieldValueInput field={field} onChange={onChange} showLabel={false} value={value} />
      </div>
    );
  }

  return (
    <div className="model-api-param-card" data-tone={tone}>
      <div className="model-api-param-card-head">
        <button
          aria-expanded={open}
          className="model-api-param-toggle"
          onClick={() => setOpen((current) => !current)}
          type="button"
        >
          <span aria-hidden className="model-api-param-caret" />
          <code>{field.name}</code>
          {field.required ? <em>required</em> : null}
        </button>
        <button
          aria-expanded={hasChildren ? open : undefined}
          className="model-api-type-chip"
          data-tone={tone}
          disabled={!hasChildren}
          onClick={() => hasChildren && setOpen((current) => !current)}
          type="button"
        >
          {field.type}
        </button>
      </div>
      {open ? (
        <div className="model-api-param-card-body">
          <p>{field.description}</p>
          {isStructuredField(field) ? (
            <FieldValueInput field={field} onChange={onChange} value={value} />
          ) : field.type.includes('boolean') ? (
            <FieldValueInput field={field} onChange={onChange} value={value} />
          ) : (
            <FieldValueInput field={field} onChange={onChange} value={value} />
          )}
          {hasChildren ? (
            <div className="model-api-param-children">
              <span>字段结构</span>
              {field.children!.map((child) => (
                <div className="model-api-param-child" key={`${field.name}.${child.name}`}>
                  <code>{child.name}</code>
                  <strong data-tone={fieldTone(child)}>{child.type}</strong>
                  <p>{child.description}</p>
                </div>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function FieldDetailList({
  depth = 0,
  fields,
  parentPath,
}: {
  depth?: number;
  fields: FieldInfo[];
  parentPath: string;
}) {
  return (
    <div className="model-api-field-popover-list" data-depth={depth}>
      {fields.map((field) => {
        const hasChildren = Boolean(field.children?.length);
        const childPath = fieldPathLabel(`${parentPath}.${field.name}`, field);

        return (
          <div className="model-api-field-popover-item" key={childPath}>
            <div className="model-api-field-popover-row">
              <div className="model-api-field-popover-name">
                <code>{field.name}</code>
                {field.required ? <span>required</span> : null}
              </div>
              <span className="model-api-field-popover-type" data-tone={fieldTone(field)}>
                {field.type}
              </span>
            </div>
            <p>{field.description}</p>
            {hasChildren ? (
              <div className="model-api-field-popover-children">
                <code>{childPath}</code>
                <FieldDetailList depth={depth + 1} fields={field.children!} parentPath={childPath} />
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function FieldDetailPopover({
  fields,
  id,
  path,
  style,
}: {
  fields: FieldInfo[];
  id: string;
  path: string;
  style?: CSSProperties;
}) {
  return (
    <div aria-label={`${path} 参数详情`} className="model-api-field-popover" id={id} role="region" style={style}>
      <div className="model-api-field-popover-title">
        <code>{path}</code>
      </div>
      <FieldDetailList fields={fields} parentPath={path} />
    </div>
  );
}

function FieldRow({ field, path }: { field: FieldInfo; path: string }) {
  const hasChildren = Boolean(field.children?.length);
  const [open, setOpen] = useState(false);
  const [popoverPosition, setPopoverPosition] = useState<FieldPopoverPosition | null>(null);
  const blockRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverId = useId();
  const tone = fieldTone(field);
  const detail = fieldDetailTarget(field, path);
  const updatePopoverPosition = useCallback(() => {
    const block = blockRef.current;
    const trigger = triggerRef.current;
    if (!block || !trigger) return;

    const blockRect = block.getBoundingClientRect();
    const triggerRect = trigger.getBoundingClientRect();
    const viewportPadding = 16;
    const width = Math.min(544, Math.max(288, window.innerWidth - viewportPadding * 2));
    const left = Math.min(
      Math.max(viewportPadding, blockRect.left + 16),
      window.innerWidth - viewportPadding - width,
    );
    const belowTop = triggerRect.bottom + 10;
    const belowSpace = window.innerHeight - belowTop - viewportPadding;
    const aboveSpace = triggerRect.top - viewportPadding;
    const maxAvailableHeight = Math.max(belowSpace, aboveSpace);
    const maxHeight = Math.min(460, Math.max(220, maxAvailableHeight));
    const top =
      belowSpace >= 220 || belowSpace >= aboveSpace
        ? belowTop
        : Math.max(viewportPadding, triggerRect.top - maxHeight - 10);

    setPopoverPosition({ left, maxHeight, top, width });
  }, []);

  useEffect(() => {
    if (!open) return;
    updatePopoverPosition();

    function handlePointerDown(event: MouseEvent) {
      const target = event.target;
      if (target instanceof Node && blockRef.current?.contains(target)) return;
      setOpen(false);
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') setOpen(false);
    }

    function handleViewportChange() {
      updatePopoverPosition();
    }

    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    document.addEventListener('scroll', handleViewportChange, true);
    window.addEventListener('resize', handleViewportChange);

    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
      document.removeEventListener('scroll', handleViewportChange, true);
      window.removeEventListener('resize', handleViewportChange);
    };
  }, [open, updatePopoverPosition]);

  return (
    <div className="model-api-field-block" data-popover-open={open ? 'true' : undefined} ref={blockRef}>
      <div className="model-api-field-row">
        <div>
          <div className="model-api-field-name">
            <code>{field.name}</code>
            {field.required ? <span>required</span> : null}
          </div>
          <p>{field.description}</p>
        </div>
        <button
          aria-controls={hasChildren ? popoverId : undefined}
          aria-expanded={hasChildren ? open : undefined}
          aria-haspopup={hasChildren ? 'dialog' : undefined}
          className="model-api-type-chip"
          data-tone={tone}
          disabled={!hasChildren}
          onClick={() => {
            if (!hasChildren) return;
            if (!open) updatePopoverPosition();
            setOpen((value) => !value);
          }}
          ref={triggerRef}
          type="button"
        >
          {field.type}
        </button>
      </div>
      {hasChildren && open ? (
        <FieldDetailPopover
          fields={detail.fields}
          id={popoverId}
          path={detail.path}
          style={
            popoverPosition
              ? {
                  left: popoverPosition.left,
                  maxHeight: popoverPosition.maxHeight,
                  top: popoverPosition.top,
                  width: popoverPosition.width,
                }
              : undefined
          }
        />
      ) : null}
    </div>
  );
}

function FieldTable({ fields }: { fields: FieldInfo[] }) {
  if (fields.length === 0) {
    return <p className="model-api-empty">该接口没有请求体参数。</p>;
  }

  return (
    <div className="model-api-fields">
      {fields.map((field) => (
        <FieldRow field={field} key={field.name} path={field.name} />
      ))}
    </div>
  );
}

function LiveResponseSummary({
  copied,
  failed,
  liveResponse,
  liveResponseView,
  onCopy,
  onChangeView,
}: {
  copied: boolean;
  failed?: boolean;
  liveResponse: LiveResponse;
  liveResponseView: LiveResponseView;
  onCopy: () => void;
  onChangeView: (view: LiveResponseView) => void;
}) {
  return (
    <div className="model-api-live-response" data-state={liveResponse.ok ? 'success' : 'error'}>
      <div className="model-api-live-status">
        <strong>{liveResponse.status === 'network' ? 'Network Error' : `${liveResponse.status} ${liveResponse.statusText}`}</strong>
        <span className="model-api-live-check">{liveResponse.ok ? '校验通过' : '需要处理'}</span>
        <span>{liveResponse.durationMs > 0 ? `${liveResponse.durationMs} ms` : '未发送'}</span>
        <span>{formatBytes(liveResponse.sizeBytes)}</span>
      </div>
      <div className="model-api-live-preview">
        <span>{liveResponse.previewLabel}</span>
        <p>{liveResponse.previewText}</p>
      </div>
      <div className="model-api-live-toolbar-row">
        <div className="model-api-live-toolbar" aria-label="响应展示模式" role="tablist">
          {(['pretty', 'raw'] as const).map((view) => (
            <button
              aria-selected={liveResponseView === view}
              key={view}
              onClick={() => onChangeView(view)}
              role="tab"
              type="button"
            >
              {view === 'pretty' ? 'Pretty' : 'Raw'}
            </button>
          ))}
        </div>
        <PanelCopyButton copied={copied} failed={failed} label="复制返回结果" onClick={onCopy} />
      </div>
    </div>
  );
}

function statusClass(status: string): string {
  if (status.startsWith('2')) return 'is-success';
  if (status.startsWith('4') || status.startsWith('5')) return 'is-error';
  return '';
}

function highlightLanguage(language: CodeLanguage): string {
  switch (language) {
    case 'curl':
      return 'bash';
    case 'csharp':
      return 'csharp';
    default:
      return language;
  }
}

export function ModelApiReference({ endpoint }: { endpoint: EndpointId }) {
  const config = endpoints[endpoint];
  const errorSamples = useMemo(() => buildErrorSamples(config), [config]);
  const [language, setLanguage] = useState<CodeLanguage>('curl');
  const [baseUrl, setBaseUrl] = useState(config.baseUrl);
  const [apiKey, setApiKey] = useState('');
  const [bodyText, setBodyText] = useState(() =>
    config.requestSample ? formatJson(config.requestSample) : '',
  );
  const [statusTab, setStatusTab] = useState('200');
  const [liveResponse, setLiveResponse] = useState<LiveResponse | null>(null);
  const [liveResponseView, setLiveResponseView] = useState<LiveResponseView>('pretty');
  const [copied, setCopied] = useState<CopyTarget | null>(null);
  const [copyError, setCopyError] = useState<CopyErrorTarget>(null);
  const [jsonEditorOpen, setJsonEditorOpen] = useState(false);
  const [sending, setSending] = useState(false);
  const code = useMemo(
    () => buildCode(language, config, baseUrl, bodyText),
    [baseUrl, bodyText, config, language],
  );
  const requestUrl = joinUrl(baseUrl, config.path);
  const bodyObject = useMemo(() => tryParseJsonObject(bodyText), [bodyText]);
  const bodyParseError = useMemo(() => {
    if (!bodyText.trim()) return '';
    try {
      parseJsonObject(bodyText);
      return '';
    } catch {
      return '当前 JSON 编辑器内容未通过校验，请修正后再发送。';
    }
  }, [bodyText]);

  const activeSample = errorSamples.find((item) => item.status === statusTab) ?? errorSamples[0];
  const liveResponseText = liveResponse
    ? liveResponseView === 'pretty'
      ? liveResponse.formattedBody
      : liveResponse.bodyText
    : '';
  const responseText = liveResponse ? liveResponseText : formatJson(activeSample.sample);

  function updateBodyField(field: FieldInfo, value: unknown) {
    const next = { ...bodyObject };
    if (value === undefined || value === '') {
      delete next[field.name];
    } else {
      next[field.name] = value;
    }
    setBodyText(formatJson(next));
  }

  async function copyMarkdown() {
    const markdown = `# ${config.title}

${config.summary}

\`${config.method} ${config.path}\`

## Request Body

${config.requestFields
  .map((field) => `- \`${field.name}\` ${field.required ? '必填' : '可选'}，${field.type}：${field.description}`)
  .join('\n')}
`;

    await copyText(markdown, 'markdown');
  }

  async function copyText(text: string, target: CopyTarget) {
    try {
      await navigator.clipboard.writeText(text);
      setCopyError(null);
      setCopied(target);
      window.setTimeout(() => setCopied(null), 1600);
    } catch (error) {
      setCopied(null);
      setCopyError(target);
      window.setTimeout(() => setCopyError(null), 1600);
      console.warn('Copy failed:', error);
    }
  }

  async function sendRequest() {
    if (!apiKey.trim()) {
      setLiveResponse(createLocalResponse('请先展开 Authorization，填写 API Key。', requestUrl));
      setLiveResponseView('pretty');
      return;
    }

    if (bodyParseError) {
      setLiveResponse(createLocalResponse(bodyParseError, requestUrl));
      setLiveResponseView('pretty');
      return;
    }

    setSending(true);
    setLiveResponse(null);
    setLiveResponseView('pretty');
    const startedAt = Date.now();
    try {
      const init: RequestInit = {
        method: config.method,
        headers: {
          Authorization: `Bearer ${apiKey.trim()}`,
        },
      };

      if (config.method === 'POST' && config.bodyFormat === 'application/json') {
        init.headers = {
          ...init.headers,
          'Content-Type': 'application/json',
        };
        init.body = bodyText;
      }

      const response = await fetch(requestUrl, init);
      const status = String(response.status);
      const text = await response.text();
      const parsedBody = parseMaybeJson(text);
      const formattedBody = parsedBody === undefined ? text : formatJson(parsedBody);
      const preview = extractResponsePreview(parsedBody, text);

      setLiveResponse({
        bodyText: text,
        durationMs: Date.now() - startedAt,
        formattedBody,
        ok: response.ok,
        parsedBody,
        previewLabel: preview.label,
        previewText: preview.text,
        requestUrl,
        sizeBytes: new Blob([text]).size,
        status,
        statusText: response.statusText || (response.ok ? 'OK' : 'Error'),
        suggestion: responseSuggestion(status, response.ok, parsedBody),
      });
      if (errorSamples.some((item) => item.status === status)) {
        setStatusTab(status);
      }
    } catch (error) {
      setLiveResponse(
        createNetworkErrorResponse(error instanceof Error ? error.message : '请求失败', requestUrl, Date.now() - startedAt),
      );
    } finally {
      setSending(false);
    }
  }

  return (
    <section className="model-api-reference not-prose">
      <div className="model-api-actions">
        <button onClick={copyMarkdown} type="button">
          {copied === 'markdown' ? <CheckIcon /> : <ClipboardIcon />}
          {copyError === 'markdown' ? '复制失败' : copied === 'markdown' ? '已复制' : '复制 Markdown'}
        </button>
      </div>

      <p className="model-api-summary">{config.summary}</p>

      <div className="model-api-grid">
        <main className="model-api-main">
          <div className="model-api-request-card">
            <div className="model-api-origin">
              <input
                aria-label="Base URL"
                onChange={(event) => setBaseUrl(event.target.value)}
                spellCheck={false}
                value={baseUrl}
              />
            </div>
            <div className="model-api-endpoint-line">
              <span>{config.method}</span>
              <code>{concretePath(config.path)}</code>
              <button disabled={sending} onClick={sendRequest} type="button">
                {sending ? 'Sending' : 'Send'}
              </button>
            </div>
            <details>
              <summary>
                <span>Authorization</span>
              <small>{config.authScheme ?? 'BearerAuth'}</small>
            </summary>
            <div className="model-api-auth-card">
              <strong>{config.authScheme ?? 'BearerAuth'}</strong>
              <p>使用 Bearer Token 认证。</p>
              <code>Authorization: Bearer sk-xxxxxx</code>
            </div>
            <label className="model-api-input">
              <span>Authorization</span>
              <input
                autoComplete="off"
                  onChange={(event) => setApiKey(event.target.value)}
                  placeholder="Bearer sk-xxxxxx"
                  type="password"
                  value={apiKey}
                />
              </label>
              <p>使用 Bearer Token 认证，格式：Authorization: Bearer sk-xxxxxx。</p>
            </details>
            <details open>
              <summary>
                <span>{config.bodyFormat === 'none' ? 'Query' : 'Body'}</span>
                <small>{config.bodyFormat}</small>
              </summary>
              {config.bodyFormat === 'application/json' ? (
                <>
                  <div className="model-api-param-toolbar">
                    <button onClick={() => setJsonEditorOpen((value) => !value)} type="button">
                      {jsonEditorOpen ? '关闭 JSON Editor' : 'Open JSON Editor'}
                    </button>
                    {bodyParseError ? <span role="status">{bodyParseError}</span> : <span>参数会同步到右侧示例代码。</span>}
                  </div>
                  <div className="model-api-param-list">
                    {config.requestFields.map((field) => (
                      <RequestFieldControl
                        field={field}
                        key={field.name}
                        onChange={(value) => updateBodyField(field, value)}
                        value={bodyObject[field.name]}
                      />
                    ))}
                  </div>
                  {jsonEditorOpen ? (
                    <label className="model-api-input model-api-json-editor">
                      <span>Request Body JSON</span>
                      <textarea
                        onChange={(event) => setBodyText(event.target.value)}
                        rows={10}
                        spellCheck={false}
                        value={bodyText}
                      />
                    </label>
                  ) : null}
                </>
              ) : (
                <p>
                  {config.bodyFormat === 'multipart/form-data'
                    ? '该接口需要使用 multipart/form-data 上传文件。'
                    : '该接口不需要请求体。'}
                </p>
              )}
            </details>
          </div>

          <section className="model-api-section">
            <div className="model-api-section-title">
              <h2 id="authorization">Authorization</h2>
              <span>{config.authScheme ?? 'BearerAuth'}</span>
            </div>
            <p>
              请求头使用 <code>Authorization</code>，值为 <code>Bearer {'<token>'}</code>。
              Token 来自控制台“API 密钥”，不是登录态，也不是管理员密钥。
            </p>
            <p>
              In: <code>header</code>
            </p>
          </section>

          <section className="model-api-section">
            <div className="model-api-section-title">
              <h2 id="request-body">{config.bodyFormat === 'none' ? 'Query' : 'Request Body'}</h2>
              <span>{config.bodyFormat}</span>
            </div>
            <FieldTable fields={config.requestFields} />
          </section>

          <section className="model-api-section">
            <div className="model-api-section-title">
              <h2 id="response-body">Response Body</h2>
              <span>application/json</span>
            </div>
            <FieldTable fields={config.responseFields} />
          </section>

          <section className="model-api-section">
            <h2 id="description">说明</h2>
            <p>{config.description}</p>
          </section>
        </main>

        <aside className="model-api-side">
          <section className="model-api-code-card" id="code-samples">
            <div className="model-api-panel-header" data-panel="code">
              <div aria-label="代码语言" className="model-api-tabs" role="tablist">
                {(Object.keys(languageLabels) as CodeLanguage[]).map((item) => (
                  <button
                    aria-selected={language === item}
                    data-language={item}
                    key={item}
                    onClick={() => setLanguage(item)}
                    role="tab"
                    type="button"
                  >
                    {languageLabels[item]}
                  </button>
                ))}
              </div>
              <PanelCopyButton
                copied={copied === 'code'}
                failed={copyError === 'code'}
                label="复制代码"
                onClick={() => copyText(code, 'code')}
              />
            </div>
            <DynamicCodeBlock
              code={code}
              codeblock={{
                allowCopy: false,
                className: 'model-api-highlight',
                viewportProps: { className: 'model-api-code-viewport' },
              }}
              lang={highlightLanguage(language)}
            />
          </section>

          <section className="model-api-response-card">
            <div className="model-api-panel-header" data-panel="response">
              {liveResponse ? (
                <div className="model-api-live-title">
                  <span>Live Response</span>
                  <strong className={statusClass(liveResponse.status)}>
                    {liveResponse.status === 'network' ? 'Network' : liveResponse.status}
                  </strong>
                </div>
              ) : (
                <div aria-label="响应状态码" className="model-api-status-tabs" role="tablist">
                  {errorSamples.map((item) => (
                    <button
                      aria-selected={statusTab === item.status}
                      className={statusClass(item.status)}
                      key={item.status}
                      onClick={() => setStatusTab(item.status)}
                      role="tab"
                      type="button"
                    >
                      {item.label}
                    </button>
                  ))}
                </div>
              )}
              {liveResponse ? null : (
                <PanelCopyButton
                  copied={copied === 'response'}
                  failed={copyError === 'response'}
                  label="复制响应"
                  onClick={() => copyText(responseText, 'response')}
                />
              )}
            </div>
            {liveResponse ? (
              <LiveResponseSummary
                copied={copied === 'response'}
                failed={copyError === 'response'}
                liveResponse={liveResponse}
                liveResponseView={liveResponseView}
                onCopy={() => copyText(responseText, 'response')}
                onChangeView={setLiveResponseView}
              />
            ) : null}
            <DynamicCodeBlock
              code={responseText}
              codeblock={{
                allowCopy: false,
                className: 'model-api-highlight',
                viewportProps: { className: 'model-api-code-viewport' },
              }}
              lang="json"
            />
          </section>
        </aside>
      </div>
    </section>
  );
}
