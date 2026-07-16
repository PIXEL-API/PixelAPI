import type { ReactNode } from 'react';

type ApiMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

type EndpointReferenceProps = {
  authentication?: string;
  children?: ReactNode;
  description: string;
  method: ApiMethod;
  path: string;
  title: string;
};

type ParameterRowProps = {
  defaultValue?: string;
  description: string;
  name: string;
  required?: boolean;
  type: string;
};

type ApiExampleProps = {
  children: ReactNode;
  label?: string;
};

const methodClassMap: Record<ApiMethod, string> = {
  GET: 'api-ref-method-get',
  POST: 'api-ref-method-post',
  PUT: 'api-ref-method-put',
  PATCH: 'api-ref-method-patch',
  DELETE: 'api-ref-method-delete',
};

export function EndpointReference({
  authentication = 'Bearer API Key',
  children,
  description,
  method,
  path,
  title,
}: EndpointReferenceProps) {
  return (
    <section className="api-ref not-prose">
      <div className="api-ref-header">
        <div>
          <span className="api-ref-kicker">Endpoint</span>
          <h3>{title}</h3>
          <p>{description}</p>
        </div>
        <div className="api-ref-auth">
          <span>Auth</span>
          <strong>{authentication}</strong>
        </div>
      </div>
      <div className="api-ref-path">
        <span className={`api-ref-method ${methodClassMap[method]}`}>{method}</span>
        <code>{path}</code>
      </div>
      {children}
    </section>
  );
}

export function ParameterTable({ children }: { children: ReactNode }) {
  return (
    <div className="api-ref-section">
      <div className="api-ref-section-title">
        <span>Parameters</span>
      </div>
      <div className="api-ref-param-list">{children}</div>
    </div>
  );
}

export function ParameterRow({
  defaultValue,
  description,
  name,
  required = false,
  type,
}: ParameterRowProps) {
  return (
    <div className="api-ref-param-row">
      <div>
        <div className="api-ref-param-name">
          <code>{name}</code>
          {required ? <em>required</em> : <span>optional</span>}
        </div>
        <p>{description}</p>
        {defaultValue ? <small>默认值：{defaultValue}</small> : null}
      </div>
      <strong>{type}</strong>
    </div>
  );
}

export function ApiExample({ children, label = 'Example' }: ApiExampleProps) {
  return (
    <div className="api-ref-section api-ref-example">
      <div className="api-ref-section-title">
        <span>{label}</span>
      </div>
      <div className="api-ref-example-body">{children}</div>
    </div>
  );
}
