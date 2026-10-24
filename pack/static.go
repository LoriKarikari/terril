package pack

import (
	"fmt"
	"path"
	"regexp"
)

var staticProvider = provider{
	name: "static",
	detect: func(f *Facts) Role {
		if f.Node == nil && f.Python == nil && f.Go == nil && f.Has("index.html") {
			return Primary
		}
		return 0
	},
	plan: func(f *Facts, _ []string) ([]Stage, Runtime, error) {
		rt, err := staticRuntime(f, Copy{Src: []string{"."}, Dst: "/srv/", Exclude: sourceExcludes}, false)
		return nil, rt, err
	},
}

// Admin and automatic HTTPS are off because the container sits behind a proxy.
const caddyfile = `{
	admin off
	auto_https off
}

:{$PORT} {
	root * /srv
	encode gzip
%s	file_server
}
`

func staticRuntime(f *Facts, site Copy, spa bool) (Runtime, error) {
	if len(f.Config.Packages) > 0 {
		return Runtime{}, fmt.Errorf("static sites run on Caddy's Alpine image, which has no apt; remove packages from wolkpack.toml")
	}
	fallback := ""
	if spa {
		fallback = "\ttry_files {path} /index.html\n"
	}
	return Runtime{
		From:    "caddy:2.11-alpine",
		Workdir: "/srv",
		Env:     []Env{{"XDG_CONFIG_HOME", "/tmp/caddy"}, {"XDG_DATA_HOME", "/tmp/caddy"}},
		Files:   []File{{"/etc/caddy/Caddyfile", fmt.Sprintf(caddyfile, fallback)}},
		Copy:    site,
		Cmd:     Cmd{"caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"},
	}, nil
}

var viteOutDirRe = regexp.MustCompile(`outDir:\s*["']([^"']+)["']`)

// viteOutDir returns where a Vite app's build writes its files, or "" if the app doesn't use Vite.
func viteOutDir(f *Facts) string {
	pkg := f.Node.Package
	if pkg.Dependencies["vite"] == "" && pkg.DevDependencies["vite"] == "" || pkg.Scripts["build"] == "" {
		return ""
	}
	for _, name := range []string{"vite.config.ts", "vite.config.js", "vite.config.mts", "vite.config.mjs"} {
		src, _ := f.read(name)
		if m := viteOutDirRe.FindStringSubmatch(src); m != nil {
			return path.Clean(m[1])
		}
	}
	return "dist"
}
