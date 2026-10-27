// Fixtures / Mock only; this module starts no server and contacts no service.
import { findUser } from './query';
export function lookup(db: { execute(sql: string): unknown }, input: string) {
  return findUser(db, input);
}
