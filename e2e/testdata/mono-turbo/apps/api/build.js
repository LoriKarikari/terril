require("fs").mkdirSync("dist", { recursive: true });
require("fs").writeFileSync(
  "dist/server.js",
  `const greet = require("@mono/greeting");
require("http").createServer((_, res) => res.end(greet("turbo"))).listen(process.env.PORT);`,
);
