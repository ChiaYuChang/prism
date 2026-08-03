export function parsePermission(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return undefined;
  const parsed = /^0x/i.test(raw) ? Number.parseInt(raw.slice(2), 16) : Number.parseInt(raw, 10);
  if (!Number.isInteger(parsed) || parsed < 0 || parsed > 255) throw new Error("permissions must be an integer from 0 through 255");
  if ((parsed & 0x1e) !== 0 || (parsed & 0x80) !== 0 && parsed !== 0x80) throw new Error("permissions contain reserved bits");
  return parsed;
}

export function permissionHex(value) {
  return `0x${Number(value || 0).toString(16).padStart(2, "0")}`;
}
