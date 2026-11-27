require("fs").mkdirSync("dist", { recursive: true });
require("fs").writeFileSync(
  "dist/server.js",
  `const greet = require("@mono/greeting");
require("http").createServer((_, res) => res.end(greet("npm"))).listen(process.env.PORT);`,
);
