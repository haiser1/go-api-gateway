import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL || 'http://gateway:8080';
const PATH = __ENV.PATH_UNDER_TEST || '/api/v1/users';
const MODE = __ENV.MODE || 'closed';
const VUS = parseInt(__ENV.VUS || '100');
const RATE = parseInt(__ENV.RATE || '1000');
const DURATION = __ENV.DURATION || '60s';

const headers = {};
if (__ENV.API_KEY) headers[__ENV.API_KEY_HEADER || 'X-API-Key'] = __ENV.API_KEY;
if (__ENV.JWT) headers['Authorization'] = `Bearer ${__ENV.JWT}`;

export const options = {
  scenarios: MODE === 'open'
    ? {
      load: {
        executor: 'constant-arrival-rate',
        rate: RATE,
        timeUnit: '1s',
        duration: DURATION,
        preAllocatedVUs: VUS,
        maxVUs: VUS * 4,
      },
    }
    : {
      load: {
        executor: 'constant-vus',
        vus: VUS,
        duration: DURATION,
      },
    },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  discardResponseBodies: true,
};

export default function () {
  const response = http.get(`${BASE}${PATH}`, { headers });
  check(response, { 'status 200': (r) => r.status === 200 });
}

export function handleSummary(data) {
  const path = __ENV.SUMMARY_PATH;
  return path ? { [path]: JSON.stringify(data, null, 2) } : {};
}
