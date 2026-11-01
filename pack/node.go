package pack

import (
	"fmt"
	"path"
	"slices"
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
	plan:     planNode,
	versions: &nodeVersions,
	version: func(f *Facts) (string, error) {
		if f.Node.Manager == "bun" {
			return "", nil
		}
		return nodeVersion(f)
	},
	assetStage: func(f *Facts, name string) (Stage, error) {
		return nodeBuild(f, name, nil, false)
	},
}

type nodeManager struct {
	image               func(f *Facts) (string, error)
	setup               func(n *Node) []Step
	install, frozen     string
	prune, cache        string
	run, start, runFile string
	exec                string
	workspacePrune      func(install, pkg string) string
}

// renovate: datasource=npm depName=corepack
const corepackVersion = "0.36.0"

var corepackYarn = func(*Node) []Step {
	return []Step{
		Env{"COREPACK_ENABLE_DOWNLOAD_PROMPT", "0"},
		Env{"COREPACK_ENABLE_AUTO_PIN", "0"},
		Run{Cmd: "npm install -g --force corepack@" + corepackVersion + " && corepack enable yarn", Caches: []string{"/root/.npm"}},
	}
}

var nodeManagers = map[string]nodeManager{
	"npm": {
		image: nodeImage, install: "npm install", frozen: "npm ci",
		prune: "npm prune --omit=dev", cache: "/root/.npm",
		run: "npm run", start: "npm start", runFile: "node", exec: "npx",
		workspacePrune: func(install, pkg string) string {
			return install + " --omit=dev --include-workspace-root --workspace=" + shellQuote(pkg)
		},
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
		run: "pnpm run", start: "npm start", runFile: "node", exec: "pnpm exec",
		workspacePrune: func(install, pkg string) string {
			return install + " --prod --filter " + shellQuote(pkg+"...")
		},
	},
	"yarn": {
		image: nodeImage, setup: corepackYarn,
		install: "yarn install", frozen: "yarn install --frozen-lockfile",
		prune: "yarn install --production --frozen-lockfile --ignore-scripts --prefer-offline", cache: "/usr/local/share/.cache/yarn",
		run: "yarn run", start: "npm start", runFile: "node", exec: "yarn",
		// Yarn 1 can't install one member alone, so other members' production dependencies stay.
		workspacePrune: func(install, _ string) string { return install + " --production" },
	},
	"yarn-berry": {
		image: nodeImage,
		setup: func(n *Node) []Step {
			// Plug'n'Play loads packages from the cache at runtime, so it has to live in the image.
			return append(corepackYarn(n), Env{"YARN_ENABLE_GLOBAL_CACHE", "false"})
		},
		install: "yarn install", frozen: "yarn install --immutable",
		prune: "yarn workspaces focus --all --production",
		run:   "yarn run", start: "npm start", runFile: "node", exec: "yarn",
		// focus leaves unused zips in the cache, so it refills an empty one.
		workspacePrune: func(_, pkg string) string {
			return "rm -rf .yarn/cache && yarn workspaces focus --production " + shellQuote(pkg)
		},
	},
	"bun": {
		image:   func(f *Facts) (string, error) { return "oven/bun:" + bunVersion(f.Node) + "-slim", nil },
		install: "bun install", frozen: "bun install --frozen-lockfile",
		prune: "rm -rf node_modules && bun install --production --frozen-lockfile", cache: "/root/.bun/install/cache",
		run: "bun run", start: "bun run start", runFile: "bun", exec: "bunx",
		workspacePrune: func(install, pkg string) string {
			return install + " --production --filter " + shellQuote(pkg)
		},
	},
}

func (m nodeManager) caches() []string {
	if m.cache == "" {
		return nil
	}
	return []string{m.cache}
}

func bunVersion(n *Node) string {
	if n.ManagerVersion != "" {
		return n.ManagerVersion
	}
	return "1"
}

// Files in node_modules that Node never loads to run an app. License files stay, because
// many licenses require shipping them. Type declarations stay when the app type-checks at
// runtime.
func nodeModulesExcludes(f *Facts) []string {
	excludes := []string{
		"**/node_modules/**/*.map",
		"**/node_modules/**/README.md",
		"**/node_modules/**/readme.md",
		"**/node_modules/**/CHANGELOG.md",
		"**/node_modules/**/HISTORY.md",
	}
	deps := f.Node.Package.Dependencies
	if deps["typescript"] == "" && deps["ts-node"] == "" && deps["tsx"] == "" {
		excludes = append(excludes, "**/node_modules/**/*.d.ts", "**/node_modules/**/*.d.mts", "**/node_modules/**/*.d.cts")
	}
	return excludes
}

// An exact version builds on the newest release of its major line. Docker Hub has trixie images
// only for releases since mid-2025, and Node keeps compatibility within a major line.
func nodeVersion(f *Facts) (string, error) {
	v, err := nodeVersions.pick(f.Node.Version, f.Today)
	major, _, _ := strings.Cut(v, ".")
	return major, err
}

// Trixie matches distroless debian13, so native modules built here load at runtime.
func nodeImage(f *Facts) (string, error) {
	v, err := nodeVersion(f)
	return "node:" + v + "-trixie-slim", err
}

func distrolessNodeImage(f *Facts) string {
	v, _ := nodeVersion(f)
	return "gcr.io/distroless/nodejs" + v + "-debian13"
}

func nodeBuild(f *Facts, name string, assets []string, prune bool) (Stage, error) {
	n := f.Node
	m, ok := nodeManagers[n.Manager]
	if !ok {
		return Stage{}, fmt.Errorf("%s isn't supported yet; use npm, pnpm, yarn or bun", n.Manager)
	}
	image, err := m.image(f)
	if err != nil {
		return Stage{}, err
	}

	var steps []Step
	if m.setup != nil {
		steps = append(steps, m.setup(n)...)
	}
	if fw := nodeFrameworkFor(f); fw != nil {
		for _, e := range fw.buildEnv {
			steps = append(steps, e)
		}
	}
	install := m.install
	if n.Lockfile != "" {
		install = m.frozen
	}
	if f.Workspace != nil {
		build, err := workspaceBuild(f, m)
		if err != nil {
			return Stage{}, err
		}
		// The install reads every member's package.json, but not their source.
		manifests := []string{"package.json"}
		for _, name := range []string{n.Lockfile, "pnpm-workspace.yaml", ".yarnrc.yml", ".yarn/releases", ".yarn/plugins", ".yarn/patches", "patches"} {
			if name != "" && f.rootHas(name) {
				manifests = append(manifests, name)
			}
		}
		for _, dir := range f.Workspace.Dirs {
			manifests = append(manifests, dir+"/package.json")
		}
		steps = append(steps,
			Copy{Src: manifests, Dst: "./", Parents: true},
			Run{Cmd: install, Caches: m.caches()},
			copySource(),
		)
		if build != "" {
			steps = append(steps, Run{Cmd: build})
		}
		if prune && m.workspacePrune != nil {
			steps = append(steps, Run{
				Cmd:    "find . -name node_modules -type d -prune -exec rm -rf {} + && " + m.workspacePrune(install, n.Package.Name),
				Caches: m.caches(),
			})
		}
		return Stage{Name: name, From: image, Steps: steps}, nil
	}
	manifests := []string{"package.json"}
	if n.Lockfile != "" {
		manifests = append(manifests, n.Lockfile)
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

func workspaceBuild(f *Facts, m nodeManager) (string, error) {
	name := f.Node.Package.Name
	if name == "" {
		return "", fmt.Errorf("%s/package.json has no name, which workspace tools need to select it", f.Workspace.App)
	}
	switch {
	case f.rootHas("turbo.json"):
		return m.exec + " turbo run build --filter=" + shellQuote(name), nil
	case f.Node.Manager == "pnpm":
		return "pnpm --filter " + shellQuote(name+"...") + " run build", nil
	}
	var builds []string
	for _, dir := range buildOrder(f.Workspace.Members, name) {
		builds = append(builds, "(cd "+shellQuote(dir)+" && "+m.run+" build)")
	}
	return strings.Join(builds, " && "), nil
}

// buildOrder lists the dirs of name and its workspace dependencies that have a build
// script, dependencies first.
func buildOrder(members map[string]Member, name string) []string {
	var order []string
	seen := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		m, ok := members[n]
		if !ok || seen[n] {
			return
		}
		seen[n] = true
		for _, d := range m.Deps {
			visit(d)
		}
		if m.Build {
			order = append(order, m.Dir)
		}
	}
	visit(name)
	return order
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

// Distroless images have no shell or package manager, so the start script must be one call to
// node, to bun on Bun, or to a package bin that package-lock.json maps to a file. check verifies
// that file is a Node script.
func nodeDistroless(f *Facts) (from string, env []Env, cmd Cmd, check *Run, ok bool) {
	start, err := nodeStart(f)
	if err != nil || f.needsDebianRuntime() {
		return "", nil, nil, nil, false
	}
	bun := f.Node.Manager == "bun"
	if script := f.Node.Package.Scripts["start"]; script != "" {
		var args []string
		if env, args, ok = directCommand(script); !ok {
			return "", nil, nil, nil, false
		}
		if bin, found := f.Node.Bins[args[0]]; found && args[0] != "node" {
			args = append([]string{"node", bin}, args[1:]...)
			check = &Run{Cmd: "case \"$(head -c 64 " + shellQuote(bin) + ")\" in '#!'*node*) ;; *) " +
				"echo " + shellQuote("terril: "+bin+" isn't a Node script, so it can't run on distroless; set start in terril.toml") + " >&2; exit 1;; esac"}
		}
		runtimeCall := args[0] == "node" || bun && args[0] == "bun"
		if !runtimeCall || len(args) < 2 {
			return "", nil, nil, nil, false
		}
		start = Cmd(args)
	}
	if bun {
		return "oven/bun:" + bunVersion(f.Node) + "-distroless", env, append(Cmd{"bun"}, start[1:]...), check, true
	}
	return distrolessNodeImage(f), env, append(Cmd{"/nodejs/bin/node"}, start[1:]...), check, true
}

func planNode(f *Facts, assets []string) ([]Stage, Runtime, error) {
	if fw := nodeFrameworkFor(f); fw != nil {
		build, err := nodeBuild(f, "build", assets, false)
		if err != nil {
			return nil, Runtime{}, err
		}
		rt := Runtime{
			From:   build.From,
			Copies: fw.copies(f),
			Env:    append([]Env{{"NODE_ENV", "production"}, {"HOST", "0.0.0.0"}}, fw.env...),
			Cmd:    append(Cmd{"node"}, fw.cmd...),
		}
		if !f.needsDebianRuntime() {
			rt.From = distrolessNodeImage(f)
			rt.Cmd[0] = "/nodejs/bin/node"
		}
		return []Stage{build}, rt, nil
	}
	build, err := nodeBuild(f, "build", assets, true)
	if err != nil {
		return nil, Runtime{}, err
	}
	start, err := nodeStart(f)
	if err != nil && f.Start == "" {
		out := f.Node.ViteOutDir
		if out == "" || f.Node.Package.Scripts["build"] == "" {
			return nil, Runtime{}, err
		}
		app := ""
		if f.Workspace != nil {
			app = f.Workspace.App
		}
		site := Copy{From: "build", Src: []string{path.Join("/app", app, out) + "/"}, Dst: "/srv/"}
		rt, err := staticRuntime(f, site, true)
		return []Stage{build}, rt, err
	}
	// Astro, Nitro and others listen on HOST, often localhost by default.
	env := []Env{{"NODE_ENV", "production"}, {"HOST", "0.0.0.0"}}
	if f.Node.PnP {
		env = append(env, Env{"NODE_OPTIONS", "--require /app/.pnp.cjs"})
	}
	rt := Runtime{
		From:   build.From,
		Copies: []Copy{{From: "build", Src: []string{"/app/"}, Dst: "/app/", Exclude: nodeModulesExcludes(f)}},
		Env:    env,
		Cmd:    start,
	}
	if from, startEnv, cmd, check, ok := nodeDistroless(f); ok {
		rt.From, rt.Cmd, rt.Env = from, cmd, append(rt.Env, startEnv...)
		if check != nil {
			build.Steps = append(build.Steps, *check)
		}
	}
	if f.Workspace != nil {
		rt.Workdir = "/app/" + f.Workspace.App
	}
	return []Stage{build}, rt, nil
}

// A Node framework whose build output runs without node_modules.
type nodeFramework struct {
	dep string
	// Start scripts that run the framework's own server. "" means no start script.
	starts   []string
	buildEnv []Env
	copies   func(f *Facts) []Copy
	env      []Env
	// Arguments to node.
	cmd Cmd
}

var nodeFrameworks = []nodeFramework{
	{
		dep:    "next",
		starts: []string{"", "next start"},
		// Undocumented, but the same as output: "standalone" in next.config.
		buildEnv: []Env{{"NEXT_PRIVATE_STANDALONE", "true"}},
		copies: func(f *Facts) []Copy {
			copies := []Copy{
				{From: "build", Src: []string{"/app/.next/standalone/"}, Dst: "/app/", Exclude: nodeModulesExcludes(f)},
				{From: "build", Src: []string{"/app/.next/static/"}, Dst: "/app/.next/static/"},
			}
			if f.Has("public") {
				copies = append(copies, Copy{From: "build", Src: []string{"/app/public/"}, Dst: "/app/public/"})
			}
			return copies
		},
		// Docker sets HOSTNAME to the container ID, and Next binds to it.
		env: []Env{{"HOSTNAME", "0.0.0.0"}},
		cmd: Cmd{"server.js"},
	},
	{
		dep:    "nuxt",
		starts: []string{"", "node .output/server/index.mjs"},
		copies: func(f *Facts) []Copy {
			return []Copy{{From: "build", Src: []string{"/app/.output/"}, Dst: "/app/.output/", Exclude: nodeModulesExcludes(f)}}
		},
		cmd: Cmd{".output/server/index.mjs"},
	},
}

func nodeFrameworkFor(f *Facts) *nodeFramework {
	n := f.Node
	if f.Workspace != nil || n.PnP || nodeManagers[n.Manager].runFile != "node" || f.Start != "" {
		return nil
	}
	for i, fw := range nodeFrameworks {
		if n.Package.Dependencies[fw.dep] != "" && slices.Contains(fw.starts, n.Package.Scripts["start"]) {
			return &nodeFrameworks[i]
		}
	}
	return nil
}
