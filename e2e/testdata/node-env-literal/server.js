require("http").createServer((_, res) => res.end("hello from " + process.env.GREETING)).listen(process.env.PORT);
