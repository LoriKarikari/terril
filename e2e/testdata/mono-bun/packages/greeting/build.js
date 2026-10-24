require("fs").mkdirSync("dist", { recursive: true });
require("fs").writeFileSync(
  "dist/index.js",
  'const ms = require("ms");\nmodule.exports = (who) => "hello from the " + who + " workspace in " + ms(60000);\n',
);
