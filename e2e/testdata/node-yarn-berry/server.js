const ms = require("ms");
require("http").createServer((_, res) => res.end("yarn berry " + ms(60000))).listen(process.env.PORT);
