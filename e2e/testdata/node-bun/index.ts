import ms from "ms";

Bun.serve({ port: Number(process.env.PORT), fetch: () => new Response(`hello from bun ${ms(60000)}`) });
