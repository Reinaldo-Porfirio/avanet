// Talks to the AvaNet running on this computer.

export class SessionExpired extends Error {}

export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

async function request(path, options) {
  const response = await fetch(path, { credentials: "same-origin", ...options });
  if (response.status === 401) throw new SessionExpired();
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new ApiError(body.error || "Algo deu errado.", response.status);
  return body;
}

const send = (method, path, body) => request(path, {
  method,
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body ?? {}),
});

export const api = {
  now: () => request("/api/now"),
  days: () => request("/api/reports"),
  report: (day) => request(`/api/reports/${encodeURIComponent(day)}`),
  settings: () => request("/api/settings"),
  saveSettings: (preferences) => send("PUT", "/api/settings", preferences),
  setAutostart: (enabled) => send("PUT", "/api/autostart", { enabled }),
  requestSpeedTest: () => request("/api/speedtest", { method: "POST" }),
  deleteHistory: (confirm) => send("POST", "/api/history/delete", { confirm }),
};
