package pack

import (
	"fmt"
	"path"
)

var phpProvider = provider{
	name: "php",
	detect: func(f *Facts) Role {
		if f.PHP == nil {
			return 0
		}
		return Primary
	},
	plan: planPHP,
}

// FrankenPHP's static build embeds PHP and its extensions in one binary, so the runtime
// needs no OS. The same binary runs Composer, so installs see the extensions the app gets.
const frankenphpVersion = "1.13.3"

// FrankenPHP rebuilds the binaries of its latest release every night under the same names, so
// pin a release that already has a newer one; its binaries no longer change. Check a new pin with
// gh attestation verify --repo php/frankenphp.
var frankenphpBuilds = []struct{ arch, asset, sha256 string }{
	{"amd64", "x86_64", "f88db9972ddac5e4c097c8b703e585255e181411687e5a1f79ca4b6328526e0b"},
	{"arm64", "aarch64", "73bd5e8b82cb48e52c49ec124f1eba03bfa5608365c770372606ec4846fb929e"},
}

const composerImage = "composer:2.10.3"

// frankenphp php-cli has no -d flag, and Composer passes -d settings to the scripts it runs.
const phpWrapper = `#!/bin/sh
while :; do
	case "$1" in
	-d) shift 2 ;;
	-d*) shift ;;
	*) break ;;
	esac
done
exec /usr/local/bin/frankenphp php-cli "$@"
`

// Admin and automatic HTTPS are off because the container sits behind a proxy. The image's
// PHP files never change, so opcache skips checking their timestamps.
const frankenphpCaddyfile = `{
	admin off
	auto_https off
	frankenphp {
		php_ini display_errors Off
		php_ini expose_php Off
		php_ini opcache.validate_timestamps 0
	}
}

:{$PORT} {
	root * %s
	encode zstd gzip
	php_server
}
`

func planPHP(f *Facts, assets []string) ([]Stage, Runtime, error) {
	if f.Start != "" || len(f.Config.Packages) > 0 {
		return nil, Runtime{}, fmt.Errorf("PHP apps run on a distroless image with FrankenPHP, which has no shell or apt; remove start and packages from wolkpack.toml")
	}
	var stages []Stage
	for _, b := range frankenphpBuilds {
		stages = append(stages, Stage{Name: "frankenphp-" + b.arch, From: "scratch", Steps: []Step{Add{
			URL:        "https://github.com/php/frankenphp/releases/download/v" + frankenphpVersion + "/frankenphp-linux-" + b.asset,
			SHA256:     b.sha256,
			Dst:        "/frankenphp",
			Executable: true,
		}}})
	}
	stages = append(stages, Stage{Name: "frankenphp", From: "frankenphp-${TARGETARCH}"})

	steps := []Step{
		Copy{From: "frankenphp", Src: []string{"/frankenphp"}, Dst: "/usr/local/bin/frankenphp"},
		File{Path: "/usr/local/bin/php", Content: phpWrapper, Executable: true},
		Copy{From: "gcr.io/distroless/static-debian13", Src: []string{"/etc/ssl/certs/"}, Dst: "/etc/ssl/certs/"},
	}
	if f.PHP.Composer {
		cache := []string{"/root/.cache/composer"}
		steps = append(steps,
			Copy{From: composerImage, Src: []string{"/usr/bin/composer"}, Dst: "/usr/local/bin/composer"},
			// Composer runs @php scripts with PHP_BINARY, which would otherwise be frankenphp itself.
			Env{"PHP_BINARY", "/usr/local/bin/php"},
			Env{"COMPOSER_ALLOW_SUPERUSER", "1"},
			Env{"COMPOSER_CACHE_DIR", cache[0]},
			Copy{Src: []string{"composer.json", "composer.lock"}, Dst: "./"},
			Run{Cmd: "composer install --no-dev --no-interaction --no-progress --prefer-dist --no-scripts --no-autoloader", Caches: cache},
			copySource(),
		)
		steps = append(steps, copyAssets(assets)...)
		steps = append(steps, Run{Cmd: "composer dump-autoload --no-dev --no-interaction --optimize", Caches: cache})
	} else {
		steps = append(steps, copySource())
		steps = append(steps, copyAssets(assets)...)
	}

	env := []Env{{"XDG_CONFIG_HOME", "/tmp/caddy"}, {"XDG_DATA_HOME", "/tmp/caddy"}}
	if f.PHP.Packages["laravel/framework"] {
		env = append(env, Env{"LOG_CHANNEL", "stderr"})
	}
	config := "/etc/frankenphp/Caddyfile"
	return append(stages, Stage{Name: "build", From: "debian:trixie-slim", Steps: steps}), Runtime{
		From:  "gcr.io/distroless/static-debian13",
		Env:   env,
		Files: []File{{Path: config, Content: fmt.Sprintf(frankenphpCaddyfile, path.Join("/app", f.PHP.DocRoot))}},
		Copies: []Copy{
			{From: "frankenphp", Src: []string{"/frankenphp"}, Dst: "/usr/local/bin/frankenphp", RootOwned: true},
			{From: "build", Src: []string{"/app/"}, Dst: "/app/"},
		},
		Cmd: Cmd{"/usr/local/bin/frankenphp", "run", "--config", config, "--adapter", "caddyfile"},
	}, nil
}
