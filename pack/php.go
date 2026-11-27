package pack

import (
	"errors"
	"fmt"
	"path"
	"slices"
)

var phpProvider = provider{
	name:     "php",
	manifest: "index.php or public/index.php",
	detect:   func(f *Facts) Role { return primaryIf(f.PHP != nil) },
	plan:     planPHP,
}

// FrankenPHP's static build embeds PHP and its extensions in one binary, so the runtime
// needs no OS. The same binary runs Composer, so installs see the extensions the app gets.
//
// FrankenPHP rebuilds the binaries of its latest release every night under the same names, so
// pin a release that already has a newer one; its binaries no longer change.
//
// pin: datasource=github-releases depName=php/frankenphp release=superseded
const frankenphpVersion = "v1.13.3"

// pin: datasource=github-release-attachments depName=php/frankenphp digestVersion=v1.13.3
const frankenphpAmd64 = "f88db9972ddac5e4c097c8b703e585255e181411687e5a1f79ca4b6328526e0b"

// pin: datasource=github-release-attachments depName=php/frankenphp digestVersion=v1.13.3
const frankenphpArm64 = "73bd5e8b82cb48e52c49ec124f1eba03bfa5608365c770372606ec4846fb929e"

func frankenphpURL(asset string) string {
	return "https://github.com/php/frankenphp/releases/download/" + frankenphpVersion + "/frankenphp-linux-" + asset
}

// pin: datasource=docker depName=composer
const composerVersion = "2.10.3"

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

// The image's PHP files never change, so opcache skips checking their timestamps.
const frankenphpCaddyfile = "{\n" + caddyGlobal + `	frankenphp {
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
	if f.needsDebianRuntime() {
		return nil, Runtime{}, errors.New("PHP apps run on a distroless image with FrankenPHP, which has no shell or apt; remove start and packages from terril.toml")
	}
	stages := archDownload("frankenphp", "frankenphp", Add{Dst: "/frankenphp", Executable: true},
		Add{URL: frankenphpURL("x86_64"), SHA256: frankenphpAmd64},
		Add{URL: frankenphpURL("aarch64"), SHA256: frankenphpArm64})

	steps := []Step{
		Copy{From: fromStage("frankenphp"), Src: []string{"/frankenphp"}, Dst: "/usr/local/bin/frankenphp"},
		File{Path: "/usr/local/bin/php", Content: phpWrapper, Executable: true},
		Copy{From: fromImage("gcr.io/distroless/static-debian13"), Src: []string{"/etc/ssl/certs/"}, Dst: "/etc/ssl/certs/"},
	}
	if f.PHP.Composer {
		cache := caches("/root/.cache/composer")
		steps = append(steps,
			Copy{From: fromImage("composer:" + composerVersion), Src: []string{"/usr/bin/composer"}, Dst: "/usr/local/bin/composer"},
			// Composer runs @php scripts with PHP_BINARY, which would otherwise be frankenphp itself.
			Env{"PHP_BINARY", "/usr/local/bin/php"},
			Env{"COMPOSER_ALLOW_SUPERUSER", "1"},
			Env{"COMPOSER_CACHE_DIR", cache[0].Target},
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

	env := slices.Clone(caddyEnv)
	if f.PHP.Packages["laravel/framework"] {
		env = append(env, Env{"LOG_CHANNEL", "stderr"})
	}
	config := "/etc/frankenphp/Caddyfile"
	return append(stages, Stage{Name: buildStage, From: fromImage("debian:trixie-slim"), Steps: steps}), Runtime{
		From:  "gcr.io/distroless/static-debian13",
		Env:   env,
		Files: []File{{Path: config, Content: fmt.Sprintf(frankenphpCaddyfile, path.Join("/app", f.PHP.DocRoot))}},
		Copies: []Copy{
			{From: fromStage("frankenphp"), Src: []string{"/frankenphp"}, Dst: "/usr/local/bin/frankenphp", RootOwned: true},
			{From: fromStage(buildStage), Src: []string{"/app/"}, Dst: "/app/"},
		},
		Cmd: Cmd{"/usr/local/bin/frankenphp", "run", "--config", config, "--adapter", "caddyfile"},
	}, nil
}
