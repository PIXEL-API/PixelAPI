import http from 'k6/http';
import { check } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const baseURL = __ENV.BASE_URL || 'http://127.0.0.1:5231';
const apiKey = __ENV.API_KEY || '';
const model = __ENV.MODEL || 'gpt-5';
const timeout = __ENV.TIMEOUT || '180s';

const nonStreamRPS = Number(__ENV.NON_STREAM_RPS || 8);
const streamRPS = Number(__ENV.STREAM_RPS || 4);
const duration = __ENV.DURATION || '3m';
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || 30);
const maxVUs = Number(__ENV.MAX_VUS || 200);

const reqDurationMs = new Trend('openai_oauth_req_duration_ms', true);
const ttftMs = new Trend('openai_oauth_ttft_ms', true);
const non2xxRate = new Rate('openai_oauth_non2xx_rate');
const streamDoneRate = new Rate('openai_oauth_stream_done_rate');

export const options = {
  scenarios: {
    non_stream: {
      executor: 'constant-arrival-rate',
      rate: nonStreamRPS,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
      exec: 'runNonStream',
      tags: { request_type: 'non_stream' },
    },
    stream: {
      executor: 'constant-arrival-rate',
      rate: streamRPS,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
      exec: 'runStream',
      tags: { request_type: 'stream' },
    },
  },
  thresholds: {
    openai_oauth_non2xx_rate: ['rate<0.01'],
    openai_oauth_req_duration_ms: ['p(95)<3000', 'p(99)<6000'],
    openai_oauth_ttft_ms: ['p(99)<1200'],
    openai_oauth_stream_done_rate: ['rate>0.99'],
  },
};

function buildHeaders() {
  const headers = {
    'Content-Type': 'application/json',
    'User-Agent': 'codex_cli_rs/0.1.0',
  };
  if (apiKey) {
    headers.Authorization = `Bearer ${apiKey}`;
  }
  return headers;
}

function buildBody(stream) {
  return JSON.stringify({
    model,
    stream,
    input: [
      {
        role: 'user',
        content: [
          {
            type: 'input_text',
            text: '请返回一句极短的话：pong',
          },
        ],
      },
    ],
    max_output_tokens: 32,
  });
}

function recordMetrics(res, stream) {
  reqDurationMs.add(res.timings.duration, { request_type: stream ? 'stream' : 'non_stream' });
  ttftMs.add(res.timings.waiting, { request_type: stream ? 'stream' : 'non_stream' });
  non2xxRate.add(res.status < 200 || res.status >= 300, { request_type: stream ? 'stream' : 'non_stream' });

  if (stream) {
    const done = !!res.body && res.body.indexOf('[DONE]') >= 0;
    streamDoneRate.add(done, { request_type: 'stream' });
  }
}

function postResponses(stream) {
  const url = `${baseURL}/v1/responses`;
  const res = http.post(url, buildBody(stream), {
    headers: buildHeaders(),
    timeout,
    tags: { endpoint: '/v1/responses', request_type: stream ? 'stream' : 'non_stream' },
  });

  check(res, {
    'status is 2xx': (r) => r.status >= 200 && r.status < 300,
  });

  recordMetrics(res, stream);
  return res;
}

export function runNonStream() {
  postResponses(false);
}

export function runStream() {
  postResponses(true);
}

export function handleSummary(data) {
  return {
    stdout: `\nOpenAI OAuth /v1/responses 基线完成\n${JSON.stringify(data.metrics, null, 2)}\n`,
    'docs/perf/openai-oauth-k6-summary.json': JSON.stringify(data, null, 2),
  };
}
