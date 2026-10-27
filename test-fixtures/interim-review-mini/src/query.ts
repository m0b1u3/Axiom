// Fixtures / Mock only: deliberately unsafe query for offline workflow tests.
export function findUser(db: { execute(sql: string): unknown }, name: string) {
  const sql = "SELECT id, name FROM users WHERE name = '" + name + "'";
  return db.execute(sql);
}
