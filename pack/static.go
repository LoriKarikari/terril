package pack

import "fmt"

var staticProvider = provider{
	name:     "static",
	manifest: "index.html",
	detect: func(f *Facts) Role {
		if f.Has("index.html") {
			return Fallback
		}
		return 0
	},
	plan: func(f *Facts, _ []string) ([]Stage, Runtime, error) {
		rt, err := staticRuntime(f, Copy{Src: []string{"."}, Dst: "/srv/", Exclude: sourceExcludes}, false)
		return nil, rt, err
	},
}

// renovate: datasource=docker depName=caddy versioning=docker
const caddyVersion = "2.11-alpine"

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
		return Runtime{}, fmt.Errorf("static sites run on a distroless image, which has no apt; remove packages from terril.toml")
	}
	if f.Start != "" {
		return Runtime{}, fmt.Errorf("static sites run on a distroless image, which has no shell for a start command; remove start from terril.toml, or the web: line from the Procfile")
	}
	fallback := ""
	if spa {
		fallback = "\ttry_files {path} /index.html\n"
	}
	// Caddy's binary is static, so it needs no OS from its Alpine image.
	return Runtime{
		From:    "gcr.io/distroless/static-debian13",
		Workdir: "/srv",
		Env:     []Env{{"XDG_CONFIG_HOME", "/tmp/caddy"}, {"XDG_DATA_HOME", "/tmp/caddy"}},
		Files:   []File{{Path: "/etc/caddy/Caddyfile", Content: fmt.Sprintf(caddyfile, fallback)}},
		Copies: []Copy{
			{From: "caddy:" + caddyVersion, Src: []string{"/usr/bin/caddy"}, Dst: "/usr/bin/caddy", RootOwned: true},
			site,
		},
		Cmd: Cmd{"caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"},
	}, nil
}
