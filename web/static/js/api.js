// Thin JSON client for the local server.

const token = window.ARTIMAN.token;

export class ApiError extends Error {}

async function call(method, url, body) {
  const opts = { method, headers: { "X-Token": token } };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(url, opts);
  } catch {
    throw new ApiError("ArtiMan ne répond plus. Relancez le programme (double-clic sur ArtiMan.exe).");
  }
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = { error: text }; }
  if (!res.ok) throw new ApiError((data && data.error) || `Erreur ${res.status}`);
  return data;
}

export const api = {
  get: (url) => call("GET", url),
  post: (url, body = {}) => call("POST", url, body),
  put: (url, body) => call("PUT", url, body),
  del: (url) => call("DELETE", url),
};

/** URL with the token, for print pages, PDFs and original documents. */
export const withToken = (url) => url + (url.includes("?") ? "&" : "?") + "t=" + encodeURIComponent(token);

export const fileURL = (kind, rel) =>
  withToken(`/files/${kind}/` + String(rel).split("/").map(encodeURIComponent).join("/"));
