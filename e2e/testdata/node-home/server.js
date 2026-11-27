const fs = require("fs");
const os = require("os");
const path = require("path");
require("http").createServer((_, res) => {
  fs.writeFileSync(path.join(os.homedir(), "probe"), "ok");
  res.end("home is writable");
}).listen(process.env.PORT);
