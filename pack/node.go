package pack

import "fmt"

var nodeProvider = provider{
	name: "node",
	detect: func(f *Facts) Role {
		if f.Node == nil {
			return 0
		}
		// A package.json alone (prettier, husky) is not a frontend build.
		if f.Node.Package.Scripts["build"] != "" {
			return Primary | Assets
		}
		return Primary
	},
	plan: planNode,
	assetStage: func(f *Facts, name string) (Stage, error) {
		return nodeBuild(f, name, nil, false)
	},
}

func nodeImage(f *Facts) (string, error) {
	v, err := pickVersion("node", f.Node.Version, []string{"26", "24", "22"}, "24")
	return "node:" + v + "-slim", err
}

var nodeInstall = map[string]struct {
	install, frozen, prune, cache string
}{
	"npm":  {"npm install", "npm ci", "npm prune --omit=dev", "/root/.npm"},
	"pnpm": {"pnpm install", "pnpm install --frozen-lockfile", "pnpm prune --prod", "/root/.local/share/pnpm/store"},
}

func nodeBuild(f *Facts, name string, assets []string, prune bool) (Stage, error) {
	n := f.Node
	cmds, ok := nodeInstall[n.Manager]
	if !ok {
		return Stage{}, fmt.Errorf("%s isn't supported yet; use npm or pnpm", n.Manager)
	}
	image, err := nodeImage(f)
	if err != nil {
		return Stage{}, err
	}

	var steps []Step
	if n.Manager == "pnpm" {
		v := n.ManagerVersion
		if v == "" {
			v = "latest"
		}
		steps = append(steps, Run{Cmd: "npm install -g pnpm@" + v, Caches: []string{"/root/.npm"}})
	}
	manifests := []string{"package.json"}
	install := cmds.install
	if n.Lockfile != "" {
		manifests = append(manifests, n.Lockfile)
		install = cmds.frozen
	}
	steps = append(steps,
		Copy{Src: manifests, Dst: "./"},
		Run{Cmd: install, Caches: []string{cmds.cache}},
		copySource(),
	)
	steps = append(steps, copyAssets(assets)...)
	if n.Package.Scripts["build"] != "" {
		steps = append(steps, Run{Cmd: n.Manager + " run build"})
	}
	if prune {
		steps = append(steps, Run{Cmd: cmds.prune, Caches: []string{cmds.cache}})
	}
	return Stage{Name: name, From: image, Steps: steps}, nil
}

func nodeStart(f *Facts) (Cmd, error) {
	pkg := f.Node.Package
	if pkg.Scripts["start"] != "" {
		return Cmd{"npm", "start"}, nil
	}
	for _, file := range []string{pkg.Main, "server.js", "index.js"} {
		if file != "" && f.Has(file) {
			return Cmd{"node", file}, nil
		}
	}
	return nil, fmt.Errorf("no start command: add a \"start\" script to package.json, or a server.js")
}

func planNode(f *Facts, assets []string) ([]Stage, error) {
	build, err := nodeBuild(f, "build", assets, true)
	if err != nil {
		return nil, err
	}
	start, err := nodeStart(f)
	if err != nil && f.Web == "" {
		return nil, err
	}
	rt := runtime(build.From,
		Copy{From: "build", Src: []string{"/app/"}, Dst: "./"},
		[]Env{{"NODE_ENV", "production"}},
		start)
	return []Stage{build, rt}, nil
}
