export type Language = 'go' | 'js' | 'ts' | 'rust';
export type Runtime = 'spinkube' | 'workerpool';

export interface FunctionSummary {
  id: string;
  name: string;
  route: string;
}

export interface ApplicationSummary {
  id: string;
  name: string;
  language: Language;
  runtime: Runtime;
  description?: string;
}

export interface Deployment {
  image: string;
  imageSizeBytes?: number;
  replicas: number;
  observedReplicas: number;
  updatedReplicas: number;
  ready: boolean;
  progressing: boolean;
  message?: string;
  namespace: string;
  serviceName: string;
  internalUrl: string;
  publicUrl?: string;
}

export interface ApplicationDetail extends ApplicationSummary {
  functions: FunctionSummary[];
  deployment?: Deployment;
}

export interface CreateApplicationInput {
  name: string;
  language: Language;
  runtime?: Runtime;
  description?: string;
}

export interface CreateFunctionInput {
  name: string;
  route?: string;
}

export interface Source {
  files: Record<string, string>;
  updatedAt?: string;
}

export type BuildStatus = 'pending' | 'running' | 'succeeded' | 'failed';

export interface Build {
  id: string;
  status: BuildStatus;
  imageRef: string;
  imageSizeBytes?: number;
  error?: string;
  createdAt: string;
  finishedAt?: string;
}

export type Point = [number, number];

export interface Series {
  points: Point[] | null;
  unit?: string;
}

export interface MetricsResponse {
  range: string;
  step: string;
  series: Record<string, Series>;
}

export interface InvokeRequest {
  method: string;
  path: string;
  query?: Record<string, string[]>;
  headers?: Record<string, string[]>;
  body?: string;
  bodyIsBase64?: boolean;
}

export interface InvokeResponse {
  status: number;
  headers: Record<string, string[]>;
  body: string;
  bodyIsBase64: boolean;
  truncated: boolean;
  durationMs: number;
}
