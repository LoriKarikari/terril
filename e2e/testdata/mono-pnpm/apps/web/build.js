const greet = require("@mono/greeting");
require("fs").mkdirSync("dist", { recursive: true });
require("fs").writeFileSync(
  "dist/server.js",
  `const text = ${JSON.stringify(greet("pnpm"))};
require("http").createServer((_, res) => res.end(text)).listen(process.env.PORT);`,
);
