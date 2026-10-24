package pack

import (
	"fmt"
	"strings"
)

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

type nodeManager struct {
	image               func(n *Node) (string, error)
	setup               func(n *Node) []Step
	install, frozen     string
	prune, cache        string
	run, start, runFile string
}

var corepackYarn = func(*Node) []Step {
	return []Step{
		Env{"COREPACK_ENABLE_DOWNLOAD_PROMPT", "0"},
		Env{"COREPACK_ENABLE_AUTO_PIN", "0"},
		Run{Cmd: "npm install -g --force corepack@0.36.0 && corepack enable yarn", Caches: []string{"/root/.npm"}},
	}
}

var nodeManagers = map[string]nodeManager{
	"npm": {
		image: nodeImage, install: "npm install", frozen: "npm ci",
		prune: "npm prune --omit=dev", cache: "/root/.npm",
		run: "npm run", start: "npm start", runFile: "node",
	},
	"pnpm": {
		image: nodeImage,
		setup: func(n *Node) []Step {
			v := n.ManagerVersion
			if v == "" {
				v = "latest"
			}
			return []Step{Run{Cmd: "npm install -g pnpm@" + v, Caches: []string{"/root/.npm"}}}
		},
		install: "pnpm install", frozen: "pnpm install --frozen-lockfile",
		prune: "pnpm prune --prod", cache: "/root/.local/share/pnpm/store",
		run: "pnpm run", start: "npm start", runFile: "node",
	},
	"yarn": {
		image: nodeImage, setup: corepackYarn,
		install: "yarn install", frozen: "yarn install --frozen-lockfile",
		prune: "yarn install --production --frozen-lockfile --ignore-scripts --prefer-offline", cache: "/usr/local/share/.cache/yarn",
		run: "yarn run", start: "npm start", runFile: "node",
	},
	"yarn-berry": {
		image: nodeImage,
		setup: func(n *Node) []Step {
			// Plug'n'Play loads packages from the cache at runtime, so it has to live in the image.
			return append(corepackYarn(n), Env{"YARN_ENABLE_GLOBAL_CACHE", "false"})
		},
		install: "yarn install", frozen: "yarn install --immutable",
		prune: "yarn workspaces focus --all --production",
		run:   "yarn run", start: "npm start", runFile: "node",
	},
	"bun": {
		image: func(n *Node) (string, error) {
			v := n.ManagerVersion
			if v == "" {
				v = "1"
			}
			return "oven/bun:" + v + "-slim", nil
		},
		install: "bun install", frozen: "bun install --frozen-lockfile",
		prune: "rm -rf node_modules && bun install --production --frozen-lockfile", cache: "/root/.bun/install/cache",
		run: "bun run", start: "bun run start", runFile: "bun",
	},
}

func (m nodeManager) caches() []string {
	if m.cache == "" {
		return nil
	}
	return []string{m.cache}
}

func nodeVersion(n *Node) (string, error) {
	return pickVersion("node", n.Version, []string{"26", "24", "22"}, "24")
}

// Trixie matches distroless debian13, so native modules built here load at runtime.
func nodeImage(n *Node) (string, error) {
	v, err := nodeVersion(n)
	return "node:" + v + "-trixie-slim", err
}

func nodeBuild(f *Facts, name string, assets []string, prune bool) (Stage, error) {
	n := f.Node
	m, ok := nodeManagers[n.Manager]
	if !ok {
		return Stage{}, fmt.Errorf("%s isn't supported yet; use npm, pnpm, yarn or bun", n.Manager)
	}
	image, err := m.image(n)
	if err != nil {
		return Stage{}, err
	}

	var steps []Step
	if m.setup != nil {
		steps = append(steps, m.setup(n)...)
	}
	manifests := []string{"package.json"}
	install := m.install
	if n.Lockfile != "" {
		manifests = append(manifests, n.Lockfile)
		install = m.frozen
	}
	if f.Has(".yarnrc.yml") {
		manifests = append(manifests, ".yarnrc.yml")
	}
	steps = append(steps,
		Copy{Src: manifests, Dst: "./"},
		Run{Cmd: install, Caches: m.caches()},
		copySource(),
	)
	steps = append(steps, copyAssets(assets)...)
	if n.Package.Scripts["build"] != "" {
		steps = append(steps, Run{Cmd: m.run + " build"})
	}
	if prune {
		steps = append(steps, Run{Cmd: m.prune, Caches: m.caches()})
	}
	return Stage{Name: name, From: image, Steps: steps}, nil
}

func nodeStart(f *Facts) (Cmd, error) {
	pkg := f.Node.Package
	m := nodeManagers[f.Node.Manager]
	if pkg.Scripts["start"] != "" {
		return Cmd(strings.Fields(m.start)), nil
	}
	for _, file := range []string{pkg.Main, "server.js", "index.js", "server.ts", "index.ts"} {
		if file != "" && f.Has(file) {
			return Cmd{m.runFile, file}, nil
		}
	}
	return nil, fmt.Errorf("no start command: add a \"start\" script to package.json, or a server.js")
}

// Distroless has no shell or npm, so it only runs a start command that is a plain node call.
func distrolessNode(f *Facts) (string, Cmd, bool) {
	v, _ := nodeVersion(f.Node)
	if f.needsDebianRuntime() || nodeManagers[f.Node.Manager].runFile != "node" || strings.Contains(v, ".") {
		return "", nil, false
	}
	start, err := nodeStart(f)
	if err != nil {
		return "", nil, false
	}
	if start[0] == "npm" {
		args := strings.Fields(f.Node.Package.Scripts["start"])
		if len(args) < 2 || args[0] != "node" || strings.ContainsAny(f.Node.Package.Scripts["start"], "&|;<>()$`'\"\\*?=") {
			return "", nil, false
		}
		start = Cmd(args)
	}
	return "gcr.io/distroless/nodejs" + v + "-debian13", append(Cmd{"/nodejs/bin/node"}, start[1:]...), true
}

func planNode(f *Facts, assets []string) ([]Stage, Runtime, error) {
	build, err := nodeBuild(f, "build", assets, true)
	if err != nil {
		return nil, Runtime{}, err
	}
	start, err := nodeStart(f)
	if err != nil && f.Start == "" {
		return nil, Runtime{}, err
	}
	env := []Env{{"NODE_ENV", "production"}}
	if f.Node.PnP {
		env = append(env, Env{"NODE_OPTIONS", "--require /app/.pnp.cjs"})
	}
	rt := Runtime{
		From: build.From,
		Copy: Copy{From: "build", Src: []string{"/app/"}, Dst: "./"},
		Env:  env,
		Cmd:  start,
	}
	if image, cmd, ok := distrolessNode(f); ok {
		rt.From, rt.Cmd = image, cmd
	}
	return []Stage{build}, rt, nil
}
