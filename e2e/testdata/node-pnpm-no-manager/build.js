const fs = require("fs");
fs.mkdirSync("dist", { recursive: true });
fs.writeFileSync(
  "dist/server.js",
  `const ms = require("ms");
require("http").createServer((_, res) => res.end("built by pnpm " + ms(60000))).listen(process.env.PORT);`,
);
