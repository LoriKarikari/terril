package pack

import (
	"fmt"
	"regexp"
	"slices"
)

var rustProvider = provider{
	name: "rust",
	detect: func(f *Facts) Role {
		if f.Rust == nil {
			return 0
		}
		return Primary
	},
	plan: planRust,
}

// renovate: datasource=docker depName=rust
const rustVersion = "1.99"

// cargo-chef turns the manifests into a recipe, so dependencies compile in a layer that a source
// edit doesn't invalidate.
//
// renovate: datasource=github-releases depName=LukeMathWalker/cargo-chef
const cargoChefVersion = "v0.1.78"

// renovate: datasource=github-release-attachments depName=LukeMathWalker/cargo-chef digestVersion=v0.1.78
const cargoChefAmd64 = "aca691abfbfbbe00d482e0ed2249eec3091b65e96a0ce92947fb5b254d48b16d"

// renovate: datasource=github-release-attachments depName=LukeMathWalker/cargo-chef digestVersion=v0.1.78
const cargoChefArm64 = "cd59b90fce5fa84c4189648fec33404ec5491791eebdb812df5e7fca8336408d"

var rustChannelRe = regexp.MustCompile(`^1\.\d+(\.\d+)?$`)

// The image's own toolchain builds the app. With a toolchain file in the source, rustup would
// download the channel it names instead, on every cold build.
var rustExcludes = append(slices.Clone(sourceExcludes), "**/target", "rust-toolchain", "rust-toolchain.toml")

func planRust(f *Facts, assets []string) ([]Stage, Runtime, error) {
	r := f.Rust
	version := rustVersion
	switch {
	case r.Version == "" || r.Version == "stable":
	case rustChannelRe.MatchString(r.Version):
		version = r.Version
	default:
		return nil, Runtime{}, fmt.Errorf("rust-toolchain pins %q; terril builds stable Rust versions like 1.99", r.Version)
	}
	image := "rust:" + version + "-trixie"
	registry := []string{"/usr/local/cargo/registry", "/usr/local/cargo/git"}

	stages := []Stage{
		{Name: "cargo-chef-amd64", From: "scratch", Steps: []Step{Add{URL: cargoChefURL("x86_64"), SHA256: cargoChefAmd64, Dst: "/cargo-chef.tar.xz"}}},
		{Name: "cargo-chef-arm64", From: "scratch", Steps: []Step{Add{URL: cargoChefURL("aarch64"), SHA256: cargoChefArm64, Dst: "/cargo-chef.tar.xz"}}},
		{Name: "cargo-chef-download", From: "cargo-chef-${TARGETARCH}"},
		{Name: "cargo-chef", From: image, Steps: []Step{
			Copy{From: "cargo-chef-download", Src: []string{"/cargo-chef.tar.xz"}, Dst: "/tmp/"},
			Run{Cmd: "tar -xJf /tmp/cargo-chef.tar.xz --strip-components=1 -C /usr/local/bin --wildcards '*/cargo-chef'"},
		}},
		{Name: "planner", From: "cargo-chef", Steps: []Step{
			Copy{Src: []string{"."}, Dst: "./", Exclude: rustExcludes},
			Run{Cmd: "cargo chef prepare --recipe-path /recipe.json"},
		}},
	}
	pkg := shellQuote([]string{r.Package})
	bin := shellQuote([]string{r.Bin})
	steps := []Step{
		Copy{From: "planner", Src: []string{"/recipe.json"}, Dst: "/recipe.json"},
		Run{Cmd: "cargo chef cook --release --locked --recipe-path /recipe.json -p " + pkg + " --bin " + bin, Caches: registry},
		Copy{Src: []string{"."}, Dst: "./", Exclude: rustExcludes},
	}
	steps = append(steps, copyAssets(assets)...)
	steps = append(steps, Run{Cmd: "cargo build --release --locked -p " + pkg + " --bin " + bin + " && cp target/release/" + bin + " /out-app", Caches: registry})

	// Rust links glibc and libgcc dynamically; distroless cc has both, plus libssl for openssl-sys.
	from := "gcr.io/distroless/cc-debian13"
	if f.needsDebianRuntime() {
		from = "debian:trixie-slim"
	}
	return append(stages, Stage{Name: "build", From: "cargo-chef", Steps: steps}), Runtime{
		From:   from,
		Copies: []Copy{{From: "build", Src: []string{"/out-app"}, Dst: "/app/app"}},
		Cmd:    Cmd{"/app/app"},
	}, nil
}

func cargoChefURL(arch string) string {
	return "https://github.com/LukeMathWalker/cargo-chef/releases/download/" + cargoChefVersion + "/cargo-chef-" + arch + "-unknown-linux-musl.tar.xz"
}
