import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const baseURL = (__ENV.BASE_URL || 'http://127.0.0.1:5231').replace(/\/$/, '');
const httpAPIKey = (__ENV.HTTP_API_KEY || '').trim();
const wsAPIKey = (__ENV.WS_API_KEY || '').trim();
const model = __ENV.MODEL || 'gpt-5.1';
const duration = __ENV.DURATION || '5m';
const timeout = __ENV.TIMEOUT || '180s';

const httpRPS = Number(__ENV.HTTP_RPS || 10);
const wsRPS = Number(__ENV.WS_RPS || 10);
const chainRPS = Number(__ENV.CHAIN_RPS || 1);
const chainRounds = Number(__ENV.CHAIN_ROUNDS || 20);
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || 40);
const maxVUs = Number(__ENV.MAX_VUS || 300);

const httpDurationMs = new Trend('openai_http_req_duration_ms', true);
const wsDurationMs = new Trend('openai_ws_req_duration_ms', true);
const wsChainDurationMs = new Trend('openai_ws_chain_round_duration_ms', true);
const wsChainTTFTMs = new Trend('openai_ws_chain_round_ttft_ms', true);
const httpNon2xxRate = new Rate('openai_http_non2xx_rate');
const wsNon2xxRate = new Rate('openai_ws_non2xx_rate');
const wsChainRoundSuccessRate = new Rate('openai_ws_chain_round_success_rate');

export const options = {
  scenarios: {
    http_baseline: {
      executor: 'constant-arrival-rate',
      exec: 'runHTTPBaseline',
      rate: httpRPS,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
      tags: { path: 'http_baseline' },
    },
    ws_baseline: {
      executor: 'constant-arrival-rate',
      exec: 'runWSBaseline',
      rate: wsRPS,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
      tags: { path: 'ws_baseline' },
    },
    ws_chain_20_rounds: {
      executor: 'constant-arrival-rate',
      exec: 'runWSChain20Rounds',
      rate: chainRPS,
      timeUnit: '1s',
      duration,
      preAllocatedVUs: Math.max(2, Math.ceil(chainRPS * 2)),
      maxVUs: Math.max(20, Math.ceil(chainRPS * 10)),
      tags: { path: 'ws_chain_20_rounds' },
    },
  },
  thresholds: {
    openai_http_non2xx_rate: ['rate<0.02'],
    openai_ws_non2xx_rate: ['rate<0.02'],
    openai_http_req_duration_ms: ['p(95)<4000', 'p(99)<7000'],
    openai_ws_req_duration_ms: ['p(95)<3000', 'p(99)<6000'],
    openai_ws_chain_round_success_rate: ['rate>0.98'],
    openai_ws_chain_round_ttft_ms: ['p(99)<1200'],
  },
};

function buildHeaders(apiKey) {
  const headers = {
    'Content-Type': 'application/json',
    'User-Agent': 'codex_cli_rs/0.98.0',
  };
  if (apiKey) {
    headers.Authorization = `Bearer ${apiKey}`;
  }
  return headers;
}

function buildBody(previousResponseID) {
  const body = {
    model,
    stream: false,
    input: [
      {
        role: 'user',
        content: [{ type: 'input_text', text: '请回复一个单词: pong' }],
      },
    ],
    max_output_tokens: 64,
  };
  if (previousResponseID) {
    body.previous_response_id = previousResponseID;
  }
  return JSON.stringify(body);
}

function postResponses(apiKey, body, tags) {
  const res = http.post(`${baseURL}/v1/responses`, body, {
    headers: buildHeaders(apiKey),
    timeout,
    tags,
  });
  check(res, {
    'status is 2xx': (r) => r.status >= 200 && r.status < 300,
  });
  return res;
}

function parseResponseID(res) {
  if (!res || !res.body) {
    return '';
  }
  try {
    const payload = JSON.parse(res.body);
    if (payload && typeof payload.id === 'string') {
      return payload.id.trim();
    }
  } catch (_) {
    return '';
  }
  return '';
}

export function runHTTPBaseline() {
  const res = postResponses(httpAPIKey, buildBody(''), { transport: 'http' });
  httpDurationMs.add(res.timings.duration, { transport: 'http' });
  httpNon2xxRate.add(res.status < 200 || res.status >= 300, { transport: 'http' });
}

export function runWSBaseline() {
  const res = postResponses(wsAPIKey, buildBody(''), { transport: 'ws_v2' });
  wsDurationMs.add(res.timings.duration, { transport: 'ws_v2' });
  wsNon2xxRate.add(res.status < 200 || res.status >= 300, { transport: 'ws_v2' });
}

// 20+ 轮续链专项，验证 previous_response_id 在长链下的稳定性与时延。
export function runWSChain20Rounds() {
  let previousResponseID = '';
  for (let round = 1; round <= chainRounds; round += 1) {
    const roundStart = Date.now();
    const res = postResponses(wsAPIKey, buildBody(previousResponseID), { transport: 'ws_v2_chain' });
    const ok = res.status >= 200 && res.status < 300;
    wsChainRoundSuccessRate.add(ok, { round: `${round}` });
    wsChainDurationMs.add(Date.now() - roundStart, { round: `${round}` });
    wsChainTTFTMs.add(res.timings.waiting, { round: `${round}` });
    wsNon2xxRate.add(!ok, { transport: 'ws_v2_chain' });
    if (!ok) {
      return;
    }
    const respID = parseResponseID(res);
    if (!respID) {
      wsChainRoundSuccessRate.add(false, { round: `${round}`, reason: 'missing_response_id' });
      return;
    }
    previousResponseID = respID;
    sleep(0.01);
  }
}

export function handleSummary(data) {
  return {
    stdout: `\nOpenAI WSv2 对比压测完成\n${JSON.stringify(data.metrics, null, 2)}\n`,
    'docs/perf/openai-ws-v2-compare-summary.json': JSON.stringify(data, null, 2),
  };
}
