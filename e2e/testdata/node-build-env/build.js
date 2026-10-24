if (!process.env.NPM_TOKEN) throw new Error("NPM_TOKEN is not set during the build");
const fs = require("fs");
fs.mkdirSync("dist", { recursive: true });
fs.writeFileSync(
  "dist/server.js",
  `const text = ${JSON.stringify("greeting: " + process.env.GREETING)};
require("http").createServer((_, res) => res.end(text)).listen(process.env.PORT);`,
);
