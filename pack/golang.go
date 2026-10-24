package pack

import (
	"fmt"
	"path/filepath"
	"strings"
)

var goProvider = provider{
	name: "go",
	detect: func(f *Facts) Role {
		if f.Go == nil {
			return 0
		}
		return Primary
	},
	plan: planGo,
}

func planGo(f *Facts, assets []string) ([]Stage, Runtime, error) {
	version := "1.25"
	if f.Go.Toolchain != nil {
		version = strings.TrimPrefix(f.Go.Toolchain.Name, "go")
	} else if f.Go.Go != nil {
		version = f.Go.Go.Version
	}
	pkg, err := goMainPackage(f)
	if err != nil {
		return nil, Runtime{}, err
	}

	mods := []string{"go.mod"}
	if f.Has("go.sum") {
		mods = append(mods, "go.sum")
	}
	caches := []string{"/go/pkg/mod", "/root/.cache/go-build"}
	steps := []Step{
		Copy{Src: mods, Dst: "./"},
		Run{Cmd: "go mod download", Caches: caches[:1]},
		copySource(),
	}
	steps = append(steps, copyAssets(assets)...)
	steps = append(steps, Run{Cmd: "CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/app " + pkg, Caches: caches})

	base := "gcr.io/distroless/static-debian12"
	if f.needsDebianRuntime() {
		base = "debian:trixie-slim"
	}
	return []Stage{{Name: "build", From: "golang:" + version, Steps: steps}}, Runtime{
		From:   base,
		Copies: []Copy{{From: "build", Src: []string{"/out/app"}, Dst: "/app/app"}},
		Cmd:    Cmd{"/app/app"},
	}, nil
}

func goMainPackage(f *Facts) (string, error) {
	if f.Has("main.go") {
		return ".", nil
	}
	dirs, _ := filepath.Glob(filepath.Join(f.Dir, "cmd", "*", "main.go"))
	switch len(dirs) {
	case 0:
		return "", fmt.Errorf("no main package: add main.go or cmd/<name>/main.go")
	case 1:
		rel, _ := filepath.Rel(f.Dir, filepath.Dir(dirs[0]))
		return "./" + filepath.ToSlash(rel), nil
	}
	var names []string
	for _, d := range dirs {
		names = append(names, filepath.Base(filepath.Dir(d)))
	}
	return "", fmt.Errorf("several main packages in cmd/ (%s); add a main.go at the root to pick one", strings.Join(names, ", "))
}
