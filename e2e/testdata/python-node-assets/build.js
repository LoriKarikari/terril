require("fs").mkdirSync("static", { recursive: true });
require("fs").writeFileSync("static/index.html", "<h1>assets built by node, served by python</h1>");
