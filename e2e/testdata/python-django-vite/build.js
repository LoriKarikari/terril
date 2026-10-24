require("fs").mkdirSync("frontend/dist", { recursive: true });
require("fs").writeFileSync("frontend/dist/app.js", "// built by node, collected by django\n");
