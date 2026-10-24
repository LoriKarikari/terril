require("http").createServer((_, res) => res.end(require("fs").readFileSync("/tmp/prepared"))).listen(process.env.PORT);
