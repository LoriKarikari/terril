const ms = require("ms");
require("http").createServer((_, res) => res.end("yarn classic " + ms(60000))).listen(process.env.PORT);
