package pack

import (
	"fmt"
	goversion "go/version"
	"path"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

var goProvider = provider{
	name:     "go",
	manifest: "go.mod",
	detect: func(f *Facts) Role {
		if f.Go == nil {
			return 0
		}
		return Primary
	},
	plan: planGo,
}

// renovate: datasource=docker depName=golang
const goVersion = "1.27"

// The go and toolchain lines in go.mod are minimums, and a newer Go builds older code the same
// way, so terril builds with its own Go unless go.mod asks for a newer one.
func goImageVersion(m *modfile.File) string {
	v := "go" + goVersion
	var wants []string
	if m.Go != nil {
		wants = append(wants, "go"+m.Go.Version)
	}
	if m.Toolchain != nil {
		wants = append(wants, m.Toolchain.Name)
	}
	for _, want := range wants {
		if lang := goversion.Lang(want); goversion.Compare(lang, v) > 0 {
			v = lang
		}
	}
	return strings.TrimPrefix(v, "go")
}

func planGo(f *Facts, assets []string) ([]Stage, Runtime, error) {
	version := goImageVersion(f.Go)
	pkg, err := goMainPackage(f.GoMains)
	if err != nil {
		return nil, Runtime{}, err
	}

	mods := []string{"go.mod"}
	if f.Has("go.sum") {
		mods = append(mods, "go.sum")
	}
	download := Run{Cmd: "go mod download", Caches: []string{"/go/pkg/mod"}}
	caches := []string{"/go/pkg/mod", "/root/.cache/go-build"}
	steps := []Step{Copy{Src: mods, Dst: "./"}, download, copySource()}
	// A replace with a local path needs that folder before modules can download.
	if slices.ContainsFunc(f.Go.Replace, func(r *modfile.Replace) bool { return modfile.IsDirectoryPath(r.New.Path) }) {
		steps = []Step{copySource(), download}
	}
	steps = append(steps, copyAssets(assets)...)

	image := "golang:" + version
	base := "gcr.io/distroless/static-debian12"
	cgo := "0"
	if f.GoCgo {
		// Trixie on both sides, so the binary links against the same glibc it runs with.
		image, base, cgo = image+"-trixie", "gcr.io/distroless/cc-debian13", "1"
	} else {
		steps = append(steps, Run{
			Cmd: "cgo=$(CGO_ENABLED=1 go list -deps -f '{{if and .CgoFiles (not .Standard)}}{{.ImportPath}} {{end}}' " + pkg + ") && " +
				`{ [ -z "$cgo" ] || { echo "terril: this app needs cgo for $cgo, which terril didn't detect. Please open an issue." >&2; exit 1; }; }`,
			Caches: caches,
		})
	}
	steps = append(steps, Run{Cmd: "CGO_ENABLED=" + cgo + " go build -trimpath -ldflags='-s -w' -o /out/app " + pkg, Caches: caches})

	if f.needsDebianRuntime() {
		base = "debian:trixie-slim"
	}
	return []Stage{{Name: "build", From: image, Steps: steps}}, Runtime{
		From:   base,
		Copies: []Copy{{From: "build", Src: []string{"/out/app"}, Dst: "/app/app"}},
		Cmd:    Cmd{"/app/app"},
	}, nil
}

func goMainPackage(mains []string) (string, error) {
	switch len(mains) {
	case 0:
		return "", fmt.Errorf("no main package: add main.go or cmd/<name>/main.go")
	case 1:
		return mains[0], nil
	}
	var names []string
	for _, m := range mains {
		names = append(names, path.Base(m))
	}
	return "", fmt.Errorf("several main packages in cmd/ (%s); add a main.go at the root to pick one", strings.Join(names, ", "))
}
