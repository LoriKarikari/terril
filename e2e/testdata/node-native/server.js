const Database = require("better-sqlite3");
const db = new Database(":memory:");
const row = db.prepare("select 'native sqlite ' || sqlite_version() as v").get();
require("http").createServer((_, res) => res.end(row.v)).listen(process.env.PORT);
