export const tokenKey = "prismctl.adminToken";

export function readToken(storage = sessionStorage) {
  return storage.getItem(tokenKey) || "";
}

export function saveToken(token, storage = sessionStorage) {
  storage.setItem(tokenKey, token.trim());
}

export function clearSession(storage = sessionStorage) {
  storage.clear();
}
