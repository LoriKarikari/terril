require("fs").mkdirSync("dist", { recursive: true });
require("fs").writeFileSync("dist/index.js", 'module.exports = (who) => "hello from the " + who + " workspace";\n');
