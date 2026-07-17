import type {
  ApplicationDetail,
  ApplicationSummary,
  Build,
  CreateApplicationInput,
  CreateFunctionInput,
  FunctionSummary,
  InvokeRequest,
  InvokeResponse,
  MetricsResponse,
  Source,
  UpdateApplicationInput
} from './types';

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      'content-type': 'application/json',
      ...(init?.headers ?? {})
    }
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status} ${res.statusText}: ${body || path}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

const appBase = (id: string) => `/api/v1/applications/${id}`;
const fnBase = (appId: string, fnId: string) => `/api/v1/applications/${appId}/functions/${fnId}`;

export const api = {
  // Applications
  listApplications: () => req<ApplicationSummary[]>('/api/v1/applications'),
  createApplication: (input: CreateApplicationInput) =>
    req<ApplicationDetail>('/api/v1/applications', {
      method: 'POST',
      body: JSON.stringify(input)
    }),
  getApplication: (id: string) => req<ApplicationDetail>(appBase(id)),
  updateApplication: (id: string, patch: UpdateApplicationInput) =>
    req<ApplicationDetail>(appBase(id), {
      method: 'PATCH',
      body: JSON.stringify(patch)
    }),
  deleteApplication: (id: string) => req<void>(appBase(id), { method: 'DELETE' }),
  deployApplication: (id: string, input: { image: string; replicas?: number }) =>
    req(appBase(id) + '/deploy', { method: 'POST', body: JSON.stringify(input) }),

  // Functions
  listFunctions: (appId: string) => req<FunctionSummary[]>(appBase(appId) + '/functions'),
  createFunction: (appId: string, input: CreateFunctionInput) =>
    req<FunctionSummary>(appBase(appId) + '/functions', {
      method: 'POST',
      body: JSON.stringify(input)
    }),
  getFunction: (appId: string, fnId: string) => req<FunctionSummary>(fnBase(appId, fnId)),
  updateFunctionRoute: (appId: string, fnId: string, route: string) =>
    req<FunctionSummary>(fnBase(appId, fnId), {
      method: 'PUT',
      body: JSON.stringify({ route })
    }),
  deleteFunction: (appId: string, fnId: string) =>
    req<void>(fnBase(appId, fnId), { method: 'DELETE' }),

  // Source
  getSource: (appId: string, fnId: string) => req<Source>(fnBase(appId, fnId) + '/source'),
  putSource: (appId: string, fnId: string, files: Record<string, string>) =>
    req<Source>(fnBase(appId, fnId) + '/source', {
      method: 'PUT',
      body: JSON.stringify({ files })
    }),
  exportSourceUrl: (appId: string, fnId: string) => fnBase(appId, fnId) + '/source.tar.gz',
  importSource: async (appId: string, fnId: string, tarGz: Blob) => {
    const res = await fetch(fnBase(appId, fnId) + '/source.tar.gz', {
      method: 'POST',
      headers: { 'content-type': 'application/gzip' },
      body: tarGz
    });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}: ${await res.text()}`);
    return (await res.json()) as Source;
  },

  // Builds (per app)
  listBuilds: (appId: string) => req<Build[]>(appBase(appId) + '/builds'),
  startBuild: (appId: string) =>
    req<Build>(appBase(appId) + '/builds', { method: 'POST' }),
  getBuild: (appId: string, buildId: string) =>
    req<Build>(appBase(appId) + `/builds/${buildId}`),
  buildLogsUrl: (appId: string, buildId: string) =>
    appBase(appId) + `/builds/${buildId}/logs`,
  applicationLogsUrl: (appId: string, follow = true, tail = 200) =>
    appBase(appId) + `/logs?follow=${follow}&tail=${tail}`,

  // Metrics
  applicationMetrics: (appId: string, range = '15m', step = '15s') =>
    req<MetricsResponse>(
      appBase(appId) + `/metrics?range=${encodeURIComponent(range)}&step=${encodeURIComponent(step)}`
    ),
  functionMetrics: (appId: string, fnId: string, range = '15m', step = '15s') =>
    req<MetricsResponse>(
      fnBase(appId, fnId) + `/metrics?range=${encodeURIComponent(range)}&step=${encodeURIComponent(step)}`
    ),
  overviewMetrics: (range = '15m', step = '30s') =>
    req<MetricsResponse>(
      `/api/v1/overview/metrics?range=${encodeURIComponent(range)}&step=${encodeURIComponent(step)}`
    ),

  // Invoke
  invokeFunction: (appId: string, fnId: string, input: InvokeRequest) =>
    req<InvokeResponse>(fnBase(appId, fnId) + '/invoke', {
      method: 'POST',
      body: JSON.stringify(input)
    })
};
