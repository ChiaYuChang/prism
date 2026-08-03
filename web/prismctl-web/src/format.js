export function formatDate(value) {
  if (!value) return "never";
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "invalid" : date.toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
}

export function formatAge(value, now = Date.now()) {
  if (!value) return "never";
  const timestamp = new Date(value).valueOf();
  if (Number.isNaN(timestamp)) return "unknown";
  const seconds = Math.max(0, Math.floor((now - timestamp) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function pretty(value) {
  return JSON.stringify(value ?? {}, null, 2);
}
