// Pembungkus fetch. Semua kegagalan menjadi ApiError { status, detail }.

const STALE_HEADER = "x-from-cache";
const OFFLINE_DETAIL = "Tidak dapat terhubung ke server.";
const staleListeners = [];

export class ApiError extends Error {
  constructor(status, detail) {
    super(detail);
    this.name = "ApiError";
    this.status = status;
    this.detail = detail;
  }
}

export function onStale(listener) {
  staleListeners.push(listener);
}

async function readBody(response) {
  try {
    return await response.json();
  } catch {
    return null;
  }
}

function detailOf(body, response) {
  if (body && typeof body.detail === "string" && body.detail !== "") {
    return body.detail;
  }
  return response.statusText || `HTTP ${response.status}`;
}

async function request(method, path, payload) {
  const init = { method, headers: { Accept: "application/json" } };
  if (payload !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(payload);
  }
  let response;
  try {
    response = await fetch(path, init);
  } catch {
    throw new ApiError(0, OFFLINE_DETAIL);
  }
  const body = await readBody(response);
  if (!response.ok) {
    throw new ApiError(response.status, detailOf(body, response));
  }
  if (response.headers.get(STALE_HEADER) === "1") {
    staleListeners.forEach((listener) => listener());
  }
  return body;
}

export const api = {
  get: (path) => request("GET", path),
  post: (path, payload) => request("POST", path, payload ?? {}),
  postEmpty: (path) => request("POST", path),
  del: (path) => request("DELETE", path),
};

export function numberPath(id, suffix) {
  return `/numbers/${encodeURIComponent(id)}${suffix ?? ""}`;
}

export function isNetworkError(error) {
  return error instanceof ApiError && error.status === 0;
}

export function messageOf(error) {
  return error instanceof ApiError ? error.detail : OFFLINE_DETAIL;
}
