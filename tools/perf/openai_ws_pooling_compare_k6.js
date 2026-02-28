import http from 'k6/http';
import { check } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const pooledBaseURL = (__ENV.POOLED_BASE_URL || 'http://127.0.0.1:5231').replace(/\/$/, '');
const oneToOneBaseURL = (__ENV.ONE_TO_ONE_BASE_URL || '').replace(/\/$/, '');
const wsAPIKey = (__ENV.WS_API_KEY || '').trim();
const model = __ENV.MODEL || 'gpt-5.1';
const timeout = __ENV.TIMEOUT || '180s';
const duration = __ENV.DURATION || '5m';
const pooledRPS = Number(__ENV.POOLED_RPS || 12);
const oneToOneRPS = Number(__ENV.ONE_TO_ONE_RPS || 12);
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || 50);
const maxVUs = Number(__ENV.MAX_VUS || 400);

const pooledDurationMs = new Trend('openai_ws_pooled_duration_ms', true);
const oneToOneDurationMs = new Trend('openai_ws_one_to_one_duration_ms', true);
const pooledTTFTMs = new Trend('openai_ws_pooled_ttft_ms', true);
const oneToOneTTFTMs = new Trend('openai_ws_one_to_one_ttft_ms', true);
const pooledNon2xxRate = new Rate('openai_ws_pooled_non2xx_rate');
const oneToOneNon2xxRate = new Rate('openai_ws_one_to_one_non2xx_rate');

export const options = {
  scenarios: {
    pooled_mode: {
      executor: 'constant-arrival-rate',
      exec: 'runPooledMode',
      rate: pooledRPS,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
      tags: { mode: 'pooled' },
    },
    one_to_one_mode: {
      executor: 'constant-arrival-rate',
      exec: 'runOneToOneMode',
      rate: oneToOneRPS,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
      tags: { mode: 'one_to_one' },
      startTime: '5s',
    },
  },
  thresholds: {
    openai_ws_pooled_non2xx_rate: ['rate<0.02'],
    openai_ws_one_to_one_non2xx_rate: ['rate<0.02'],
    openai_ws_pooled_duration_ms: ['p(95)<3000', 'p(99)<6000'],
    openai_ws_one_to_one_duration_ms: ['p(95)<6000', 'p(99)<10000'],
  },
};

function buildHeaders() {
  const headers = {
    'Content-Type': 'application/json',
    'User-Agent': 'codex_cli_rs/0.98.0',
  };
  if (wsAPIKey) {
    headers.Authorization = `Bearer ${wsAPIKey}`;
  }
  return headers;
}

function buildBody() {
  return JSON.stringify({
    model,
    stream: false,
    input: [
      {
        role: 'user',
        content: [{ type: 'input_text', text: '请回复: pong' }],
      },
    ],
    max_output_tokens: 48,
  });
}

function send(baseURL, mode) {
  if (!baseURL) {
    return null;
  }
  const res = http.post(`${baseURL}/v1/responses`, buildBody(), {
    headers: buildHeaders(),
    timeout,
    tags: { mode },
  });
  check(res, {
    'status is 2xx': (r) => r.status >= 200 && r.status < 300,
  });
  return res;
}

export function runPooledMode() {
  const res = send(pooledBaseURL, 'pooled');
  if (!res) {
    return;
  }
  pooledDurationMs.add(res.timings.duration, { mode: 'pooled' });
  pooledTTFTMs.add(res.timings.waiting, { mode: 'pooled' });
  pooledNon2xxRate.add(res.status < 200 || res.status >= 300, { mode: 'pooled' });
}

export function runOneToOneMode() {
  if (!oneToOneBaseURL) {
    return;
  }
  const res = send(oneToOneBaseURL, 'one_to_one');
  if (!res) {
    return;
  }
  oneToOneDurationMs.add(res.timings.duration, { mode: 'one_to_one' });
  oneToOneTTFTMs.add(res.timings.waiting, { mode: 'one_to_one' });
  oneToOneNon2xxRate.add(res.status < 200 || res.status >= 300, { mode: 'one_to_one' });
}

export function handleSummary(data) {
  return {
    stdout: `\nOpenAI WS 池化 vs 1:1 对比压测完成\n${JSON.stringify(data.metrics, null, 2)}\n`,
    'docs/perf/openai-ws-pooling-compare-summary.json': JSON.stringify(data, null, 2),
  };
}
