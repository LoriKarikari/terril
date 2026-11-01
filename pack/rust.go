package pack

import "slices"

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

// With a toolchain file in its source, the planner would download that Rust on every source edit.
// The build stage installs it from the file, in its own layer.
var rustExcludes = append(slices.Clone(sourceExcludes), "**/target", "rust-toolchain", "rust-toolchain.toml")

func planRust(f *Facts, assets []string) ([]Stage, Runtime, error) {
	r := f.Rust
	image := "rust:" + rustVersion + "-trixie"
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
	pkg := shellQuote(r.Package)
	bin := shellQuote(r.Bin)
	var steps []Step
	// Docker Hub has trixie images only for Rust 1.88 and newer, and none for nightlies. rustup
	// installs any toolchain, with the targets and components the file lists.
	if r.Toolchain != "" {
		steps = []Step{Copy{Src: []string{r.Toolchain}, Dst: "./"}, Run{Cmd: "rustup toolchain install"}}
	}
	steps = append(steps,
		Copy{From: "planner", Src: []string{"/recipe.json"}, Dst: "/recipe.json"},
		Run{Cmd: "cargo chef cook --release --locked --recipe-path /recipe.json -p " + pkg + " --bin " + bin, Caches: registry},
		Copy{Src: []string{"."}, Dst: "./", Exclude: rustExcludes},
	)
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
