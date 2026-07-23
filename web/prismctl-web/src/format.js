export function formatDate(value) {
  if (!value) return "never";
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "invalid" : date.toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
}

export function pretty(value) {
  return JSON.stringify(value ?? {}, null, 2);
}
