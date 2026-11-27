package pack

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

var nodeProvider = provider{
	name:     "node",
	manifest: "package.json",
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
	runtime: &runtime{versions: &nodeVersions, version: func(f *Facts) (string, error) {
		if f.Node.Manager == NodeBun {
			return "", nil
		}
		return nodeVersion(f)
	}},
	assetStage: nodeAssetStage,
}

const assetsDir = "/assets"

// The build writes its output among the sources and the install's files, such as Yarn's
// .pnp.cjs, so only the files it wrote after the stamp go to the app.
func nodeAssetStage(f *Facts, name string) (Stage, error) {
	s, err := nodeBuild(f, name, nodeFrameworkFor(f))
	if err != nil {
		return s, err
	}
	build, ok := s.Steps[len(s.Steps)-1].(Run)
	if !ok {
		return s, fmt.Errorf("%s/package.json has no build script", f.Dir)
	}
	build.Cmd = "touch /tmp/before-build && " + build.Cmd + " && mkdir " + assetsDir +
		" && find . -path ./node_modules -prune -o -type f -newer /tmp/before-build -print0 | xargs -0r cp --parents -t " + assetsDir
	s.Steps[len(s.Steps)-1] = build
	return s, nil
}

type nodeManager struct {
	image           func(f *Facts) (string, error)
	setup           func(n *Node) []Step
	install, frozen string
	prune, cache    string
	// runScript runs a package.json script, startCmd runs the start script, fileRunner runs a
	// JavaScript file, and binRunner runs a package bin.
	runScript, startCmd, fileRunner string
	binRunner                       string
	workspacePrune                  func(install, pkg string) string
}

// pin: datasource=npm depName=corepack
const corepackVersion = "0.36.0"

// pin: datasource=npm depName=pnpm
const pnpmVersion = "12.23.0"

func corepackYarn(*Node) []Step {
	return []Step{
		Env{"COREPACK_ENABLE_DOWNLOAD_PROMPT", "0"},
		Env{"COREPACK_ENABLE_AUTO_PIN", "0"},
		Run{Cmd: "npm install -g --force corepack@" + corepackVersion + " && corepack enable yarn", Caches: caches("/root/.npm")},
	}
}

var nodeManagers = map[NodeManager]nodeManager{
	NodeNPM: {
		image: nodeImage, install: "npm install", frozen: "npm ci",
		prune: "npm prune --omit=dev", cache: "/root/.npm",
		runScript: "npm run", startCmd: "npm start", fileRunner: "node", binRunner: "npx",
		workspacePrune: func(install, pkg string) string {
			return install + " --omit=dev --include-workspace-root --workspace=" + shellQuote(pkg)
		},
	},
	NodePNPM: {
		image: nodeImage,
		setup: func(n *Node) []Step {
			v := n.ManagerVersion
			if v == "" {
				v = pnpmVersion
			}
			return []Step{Run{Cmd: "npm install -g pnpm@" + v, Caches: caches("/root/.npm")}}
		},
		install: "pnpm install", frozen: "pnpm install --frozen-lockfile",
		prune: "pnpm prune --prod", cache: "/root/.local/share/pnpm/store",
		runScript: "pnpm run", startCmd: "npm start", fileRunner: "node", binRunner: "pnpm exec",
		workspacePrune: func(install, pkg string) string {
			return install + " --prod --filter " + shellQuote(pkg+"...")
		},
	},
	NodeYarn: {
		image: nodeImage, setup: corepackYarn,
		install: "yarn install", frozen: "yarn install --frozen-lockfile",
		prune: "yarn install --production --frozen-lockfile --ignore-scripts --prefer-offline", cache: "/usr/local/share/.cache/yarn",
		runScript: "yarn run", startCmd: "npm start", fileRunner: "node", binRunner: "yarn",
		// Yarn 1 can't install one member alone, so other members' production dependencies stay.
		workspacePrune: func(install, _ string) string { return install + " --production" },
	},
	NodeYarnBerry: {
		image: nodeImage,
		setup: func(n *Node) []Step {
			// Plug'n'Play loads packages from the cache at runtime, so it has to live in the image.
			return append(corepackYarn(n), Env{"YARN_ENABLE_GLOBAL_CACHE", "false"})
		},
		install: "yarn install", frozen: "yarn install --immutable",
		// focus leaves dev dependency zips in the cache, so it refills an empty one.
		prune:     "rm -rf .yarn/cache && yarn workspaces focus --all --production",
		runScript: "yarn run", startCmd: "npm start", fileRunner: "node", binRunner: "yarn",
		workspacePrune: func(_, pkg string) string {
			return "rm -rf .yarn/cache && yarn workspaces focus --production " + shellQuote(pkg)
		},
	},
	NodeBun: {
		image:   func(f *Facts) (string, error) { return "oven/bun:" + bunVersion(f.Node) + "-slim", nil },
		install: "bun install", frozen: "bun install --frozen-lockfile",
		prune: "rm -rf node_modules && bun install --production --frozen-lockfile", cache: "/root/.bun/install/cache",
		runScript: "bun run", startCmd: "bun run start", fileRunner: "bun", binRunner: "bunx",
		workspacePrune: func(install, pkg string) string {
			return install + " --production --filter " + shellQuote(pkg)
		},
	},
}

func (m nodeManager) caches() []Cache {
	if m.cache == "" {
		return nil
	}
	return caches(m.cache)
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

func distrolessNodeImage(f *Facts) (string, error) {
	v, err := nodeVersion(f)
	return "gcr.io/distroless/nodejs" + v + "-debian13", err
}

// nodeBuild installs and builds the app. The caller adds the production prune when the image
// keeps node_modules.
func nodeBuild(f *Facts, name string, fw *nodeFramework) (Stage, error) {
	n := f.Node
	m := nodeManagers[n.Manager]
	image, err := m.image(f)
	if err != nil {
		return Stage{}, err
	}
	var steps []Step
	if m.setup != nil {
		steps = append(steps, m.setup(n)...)
	}
	if fw != nil {
		for _, e := range fw.buildEnv {
			steps = append(steps, e)
		}
	}
	if f.Workspace != nil {
		steps, err = nodeWorkspaceBuild(f, m, steps)
	} else {
		steps = nodePackageBuild(f, m, steps)
	}
	return Stage{Name: name, From: fromImage(image), Steps: steps}, err
}

func nodeInstall(n *Node, m nodeManager) string {
	if n.Lockfile != "" {
		return m.frozen
	}
	return m.install
}

func nodeWorkspaceBuild(f *Facts, m nodeManager, steps []Step) ([]Step, error) {
	n := f.Node
	build, err := workspaceBuild(f, m)
	if err != nil {
		return nil, err
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
		Run{Cmd: nodeInstall(n, m), Caches: m.caches()},
		copySource(),
	)
	if build != "" {
		steps = append(steps, Run{Cmd: build})
	}
	return steps, nil
}

func nodePackageBuild(f *Facts, m nodeManager, steps []Step) []Step {
	n := f.Node
	manifests := []string{"package.json"}
	if n.Lockfile != "" {
		manifests = append(manifests, n.Lockfile)
	}
	if f.Has(".yarnrc.yml") {
		manifests = append(manifests, ".yarnrc.yml")
	}
	steps = append(steps,
		Copy{Src: manifests, Dst: "./"},
		Run{Cmd: nodeInstall(n, m), Caches: m.caches()},
		copySource(),
	)
	if n.Package.Scripts["build"] != "" {
		steps = append(steps, Run{Cmd: m.runScript + " build"})
	}
	return steps
}

// nodePrune drops dev dependencies from node_modules. A workspace prunes to the app's own
// dependencies.
func nodePrune(f *Facts) Run {
	m := nodeManagers[f.Node.Manager]
	if f.Workspace != nil {
		return Run{
			Cmd:    "find . -name node_modules -type d -prune -exec rm -rf {} + && " + m.workspacePrune(nodeInstall(f.Node, m), f.Node.Package.Name),
			Caches: m.caches(),
		}
	}
	return Run{Cmd: m.prune, Caches: m.caches()}
}

func workspaceBuild(f *Facts, m nodeManager) (string, error) {
	name := f.Node.Package.Name
	if name == "" {
		return "", fmt.Errorf("%s/package.json has no name, which workspace tools need to select it", f.Workspace.App)
	}
	switch {
	case f.rootHas("turbo.json"):
		return m.binRunner + " turbo run build --filter=" + shellQuote(name), nil
	case f.Node.Manager == NodePNPM:
		return "pnpm --filter " + shellQuote(name+"...") + " run build", nil
	}
	var builds []string
	for _, dir := range buildOrder(f.Workspace.Members, name) {
		builds = append(builds, "(cd "+shellQuote(dir)+" && "+m.runScript+" build)")
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
		return Cmd(strings.Fields(m.startCmd)), nil
	}
	for _, file := range []string{pkg.Main, "server.js", "index.js", "server.ts", "index.ts"} {
		if file != "" && f.Has(file) {
			return Cmd{m.fileRunner, file}, nil
		}
	}
	return nil, errors.New("no start command: add a \"start\" script to package.json, or a server.js")
}

// Astro, Nitro and others listen on HOST, often localhost by default.
var nodeEnv = []Env{{"NODE_ENV", "production"}, {"HOST", "0.0.0.0"}}

// A distroless start runs node, or bun on Bun, with the arguments of start. check verifies at
// build time that a package bin the start script calls is a Node script.
type distrolessStart struct {
	args  []string
	env   []Env
	check []Step
}

// Distroless images have no shell or package manager, so the start script must be one call to
// node, to bun on Bun, or to a package bin that package-lock.json maps to a file.
func nodeDistrolessStart(f *Facts, start Cmd) (distrolessStart, bool) {
	script := f.Node.Package.Scripts["start"]
	if script == "" {
		return distrolessStart{args: start[1:]}, true
	}
	env, args, ok := directCommand(script)
	if !ok {
		return distrolessStart{}, false
	}
	var check []Step
	if bin, found := f.Node.Bins[args[0]]; found && args[0] != "node" {
		args = append([]string{"node", bin}, args[1:]...)
		check = []Step{Run{Cmd: "case \"$(head -c 64 " + shellQuote(bin) + ")\" in '#!'*node*) ;; *) " +
			"echo " + shellQuote("terril: "+bin+" isn't a Node script, so it can't run on distroless; set start in terril.toml") + " >&2; exit 1;; esac"}}
	}
	runtimeCall := args[0] == "node" || f.Node.Manager == NodeBun && args[0] == "bun"
	if !runtimeCall || len(args) < 2 {
		return distrolessStart{}, false
	}
	return distrolessStart{args: args[1:], env: env, check: check}, true
}

func planNode(f *Facts, _ []string) ([]Stage, Runtime, error) {
	if fw := nodeFrameworkFor(f); fw != nil {
		return nodeFrameworkPlan(f, fw)
	}
	build, err := nodeBuild(f, buildStage, nil)
	if err != nil {
		return nil, Runtime{}, err
	}
	build.Steps = append(build.Steps, nodePrune(f))
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
		site := Copy{From: fromStage(buildStage), Src: []string{path.Join("/app", app, out) + "/"}, Dst: "/srv/"}
		rt, err := staticRuntime(f, site, true)
		return []Stage{build}, rt, err
	}
	env := slices.Clone(nodeEnv)
	if f.Node.PnP {
		env = append(env, Env{"NODE_OPTIONS", "--require /app/.pnp.cjs"})
	}
	rt := Runtime{
		From:   build.From.Image,
		Copies: []Copy{{From: fromStage(buildStage), Src: []string{"/app/"}, Dst: "/app/", Exclude: nodeModulesExcludes(f)}},
		Env:    env,
		Cmd:    start,
	}
	if err != nil || f.needsDebianRuntime() {
		return []Stage{build}, nodeWorkdir(f, rt), nil
	}
	if ds, ok := nodeDistrolessStart(f, start); ok {
		if f.Node.Manager == NodeBun {
			rt.From, rt.Cmd = "oven/bun:"+bunVersion(f.Node)+"-distroless", append(Cmd{"bun"}, ds.args...)
		} else {
			if rt.From, err = distrolessNodeImage(f); err != nil {
				return nil, Runtime{}, err
			}
			rt.Cmd = append(Cmd{"/nodejs/bin/node"}, ds.args...)
		}
		rt.Env = append(rt.Env, ds.env...)
		build.Steps = append(build.Steps, ds.check...)
	}
	return []Stage{build}, nodeWorkdir(f, rt), nil
}

func nodeWorkdir(f *Facts, rt Runtime) Runtime {
	if f.Workspace != nil {
		rt.Workdir = "/app/" + f.Workspace.App
	}
	return rt
}

func nodeFrameworkPlan(f *Facts, fw *nodeFramework) ([]Stage, Runtime, error) {
	build, err := nodeBuild(f, buildStage, fw)
	if err != nil {
		return nil, Runtime{}, err
	}
	rt := Runtime{
		From:   build.From.Image,
		Copies: fw.copies(f),
		Env:    slices.Concat(nodeEnv, fw.env),
		Cmd:    append(Cmd{"node"}, fw.nodeArgs...),
	}
	if !f.needsDebianRuntime() {
		if rt.From, err = distrolessNodeImage(f); err != nil {
			return nil, Runtime{}, err
		}
		rt.Cmd[0] = "/nodejs/bin/node"
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
	nodeArgs []string
}

var nodeFrameworks = []nodeFramework{
	{
		dep:    "next",
		starts: []string{"", "next start"},
		// Undocumented, but the same as output: "standalone" in next.config.
		buildEnv: []Env{{"NEXT_PRIVATE_STANDALONE", "true"}},
		copies: func(f *Facts) []Copy {
			copies := []Copy{
				{From: fromStage(buildStage), Src: []string{"/app/.next/standalone/"}, Dst: "/app/", Exclude: nodeModulesExcludes(f)},
				{From: fromStage(buildStage), Src: []string{"/app/.next/static/"}, Dst: "/app/.next/static/"},
			}
			if f.Has("public") {
				copies = append(copies, Copy{From: fromStage(buildStage), Src: []string{"/app/public/"}, Dst: "/app/public/"})
			}
			return copies
		},
		// Docker sets HOSTNAME to the container ID, and Next binds to it.
		env:      []Env{{"HOSTNAME", "0.0.0.0"}},
		nodeArgs: []string{"server.js"},
	},
	{
		dep:    "nuxt",
		starts: []string{"", "node .output/server/index.mjs"},
		copies: func(f *Facts) []Copy {
			return []Copy{{From: fromStage(buildStage), Src: []string{"/app/.output/"}, Dst: "/app/.output/", Exclude: nodeModulesExcludes(f)}}
		},
		nodeArgs: []string{".output/server/index.mjs"},
	},
}

func nodeFrameworkFor(f *Facts) *nodeFramework {
	n := f.Node
	if f.Workspace != nil || n.PnP || n.Manager == NodeBun || f.Start != "" {
		return nil
	}
	for i, fw := range nodeFrameworks {
		if n.Package.Dependencies[fw.dep] != "" && slices.Contains(fw.starts, n.Package.Scripts["start"]) {
			return &nodeFrameworks[i]
		}
	}
	return nil
}
