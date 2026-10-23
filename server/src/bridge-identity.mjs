// Public bridge identity is independent of account credentials and pairing tokens.
import { randomUUID } from "node:crypto";
import { constants, mkdirSync, lstatSync, openSync, fstatSync, readFileSync, writeFileSync, fsyncSync, fchmodSync, closeSync, linkSync, unlinkSync } from "node:fs";
import { resolve, dirname } from "node:path";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
function noSymlinks(path, allowMissing = false) {
  for (let current = resolve(path); current !== dirname(current); current = dirname(current)) {
    try {
      if (lstatSync(current).isSymbolicLink()) throw new Error("Bridge identity refuses symlink paths");
    } catch (error) { if (!allowMissing || error.code !== "ENOENT") throw error; }
  }
}
function readIdentity(path) {
  noSymlinks(path);
  if (!lstatSync(path).isFile()) throw new Error("Invalid bridge identity file");
  const fd = openSync(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0) | (constants.O_NONBLOCK ?? 0));
  try {
    const stat = fstatSync(fd);
    if (!stat.isFile() || stat.size > 128) throw new Error("Invalid bridge identity file");
    const value = readFileSync(fd, "utf8").trim();
    if (!UUID.test(value)) throw new Error("Invalid bridge identity UUID; restore the original bridge-id file");
    fchmodSync(fd, 0o600);
    return value.toLowerCase();
  } finally { closeSync(fd); }
}

/** Publish complete bytes exclusively; concurrent callers all return the winning UUID. */
export function loadBridgeIdentity(dataDir) {
  if (typeof dataDir !== "string" || !dataDir) throw new TypeError("dataDir is required");
  const directory = resolve(dataDir), path = resolve(directory, "bridge-id");
  noSymlinks(directory, true);
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  try { return readIdentity(path); }
  catch (error) { if (error.code !== "ENOENT") throw error; }
  const id = randomUUID(), temporary = `${path}.${randomUUID()}.tmp`;
  let fd;
  try {
    fd = openSync(temporary, "wx", 0o600);
    writeFileSync(fd, `${id}\n`); fsyncSync(fd); closeSync(fd); fd = undefined;
    noSymlinks(directory);
    try { linkSync(temporary, path); }
    catch (error) { if (error.code !== "EEXIST") throw error; }
    const dirFd = openSync(directory, constants.O_RDONLY);
    try { fsyncSync(dirFd); } finally { closeSync(dirFd); }
    return readIdentity(path);
  } finally {
    if (fd !== undefined) closeSync(fd);
    try { unlinkSync(temporary); } catch (error) { if (error.code !== "ENOENT") throw error; }
  }
}
