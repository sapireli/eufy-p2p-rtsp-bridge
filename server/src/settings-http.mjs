import { timingSafeEqual, randomBytes } from 'node:crypto';
import { readFileSync, writeFileSync, mkdirSync, chmodSync } from 'node:fs';
import { join } from 'node:path';

export function loadAdminToken(dataDir) {
  mkdirSync(dataDir, { recursive: true });
  const path = join(dataDir, 'settings-admin-token');
  try { writeFileSync(path, `${randomBytes(24).toString('hex')}\n`, { mode: 0o600, flag: 'wx' }); }
  catch (error) { if (error.code !== 'EEXIST') throw error; }
  chmodSync(path, 0o600);
  const token = readFileSync(path, 'utf8').trim();
  if (!/^[a-f0-9]{48}$/.test(token)) throw new Error(`Invalid settings admin token file: ${path}`);
  return token;
}
export function authorizedSettings(req, token) {
  const actual = Buffer.from(req.headers.authorization ?? '');
  const expected = Buffer.from(`Bearer ${token}`);
  return actual.length === expected.length && timingSafeEqual(actual, expected);
}
async function readJson(req) {
  const chunks = []; let bytes = 0;
  for await (const chunk of req) {
    bytes += chunk.length;
    if (bytes > 1024 * 1024) throw Object.assign(new Error('Settings request exceeds 1 MiB'), { statusCode: 413 });
    chunks.push(chunk);
  }
  try {
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    if (!body || typeof body !== 'object' || Array.isArray(body)) throw new Error('object required');
    return body;
  }
  catch { throw Object.assign(new Error('Invalid JSON body'), { statusCode: 400 }); }
}
export function createSettingsHttp(ctx) {
  return async (req, res, url, json) => {
    res.setHeader('cache-control', 'no-store');
    if (!authorizedSettings(req, ctx.settingsAdminToken)) {
      res.setHeader('www-authenticate', 'Bearer realm="Bridge settings"');
      return json(res, 401, { error: 'Pair this client using the bridge settings admin token' });
    }
    try {
      if (url.pathname === '/api/settings' && req.method === 'GET') return json(res, 200, ctx.settings.snapshot());
      if (url.pathname === '/api/settings' && req.method === 'PATCH') return json(res, 200, await ctx.settings.patch(await readJson(req)));
      if (url.pathname === '/api/settings/export' && req.method === 'GET') {
        res.setHeader('content-disposition', 'attachment; filename="eufy-wall-settings.json"');
        return json(res, 200, ctx.settingsStore.export());
      }
      if (url.pathname === '/api/settings/import' && req.method === 'POST') return json(res, 200, await ctx.settings.import(await readJson(req)));
      if (url.pathname === '/api/settings/restart' && req.method === 'POST') {
        const { revision } = await readJson(req);
        if (revision !== ctx.settings.snapshot().revision) return json(res, 409, { error: 'Settings changed; refresh before restarting' });
        // Only accept this action when the service supervisor can start us again.
        if (!ctx.requestSettingsRestart) return json(res, 409, { error: 'Automatic restart unavailable; restart the bridge process to apply pending settings' });
        res.once('finish', () => ctx.requestSettingsRestart());
        json(res, 202, { restarting: true });
        return;
      }
      return json(res, 405, { error: 'Unsupported settings endpoint or method' });
    } catch (error) {
      const code = error.statusCode ?? (error.name === 'SettingsConflictError' ? 409 : error.name === 'SettingsValidationError' ? 400 : 500);
      // Never include a request/backup body or stream URL in logs or response errors.
      return json(res, code, { error: code === 500 ? 'Could not save bridge settings' : error.message });
    }
  };
}
