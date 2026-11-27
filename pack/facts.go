package pack

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/bmatcuk/doublestar/v4"
	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

type Facts struct {
	Dir string
	// The day to check end-of-life dates against.
	Today       time.Time
	Workspace   *Workspace
	UvWorkspace *Workspace
	Node        *Node
	Python      *Python
	Ruby        *Ruby
	PHP         *PHP
	DotNet      *DotNet
	Rust        *Rust
	Go          *modfile.File
	GoCgo       bool
	// "." when main.go is at the root, otherwise ./cmd/<name> for each cmd/<name>/main.go.
	GoMains []string
	Config  Config
	// From terril.toml, else the Procfile web: line.
	Start string
	// Why a provider can't build this app, keyed by provider name. Only the provider that builds
	// fails on it, so a fastlane Gemfile.lock doesn't stop a Node app.
	problems map[string]error
	// The first file that exists but couldn't be read.
	readErr error
}

func (f *Facts) cantBuild(provider string, err error) {
	if f.problems == nil {
		f.problems = map[string]error{}
	}
	f.problems[provider] = err
}

// readFile reports false for a file that isn't there. Any other failure is kept for Read to
// return, so an unreadable go.mod isn't reported as a missing one.
func (f *Facts) readFile(p string) (string, bool) {
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) && f.readErr == nil {
		f.readErr = err
	}
	return string(b), err == nil
}

func (f *Facts) Has(name string) bool {
	return fileExists(filepath.Join(f.Dir, name))
}

func (f *Facts) read(name string) (string, bool) {
	return f.readFile(filepath.Join(f.Dir, name))
}

func (f *Facts) root() string {
	if f.Workspace != nil {
		return f.Workspace.Root
	}
	return f.Dir
}

func (f *Facts) rootHas(name string) bool {
	return fileExists(filepath.Join(f.root(), name))
}

func (f *Facts) rootRead(name string) (string, bool) {
	return f.readFile(filepath.Join(f.root(), name))
}

func (f *Facts) firstLine(dir string, names ...string) string {
	for _, n := range names {
		if s, ok := f.readFile(filepath.Join(dir, n)); ok {
			return strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
		}
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func Read(dir string) (*Facts, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	f := &Facts{Dir: dir, Today: time.Now().UTC()}
	// The rename hint goes first, so no other error hides it.
	if f.Has("wolkpack.toml") {
		return nil, fmt.Errorf("wolkpack.toml is now terril.toml; rename the file")
	}
	err := f.readAll()
	// A file that couldn't be read explains any error after it, such as a missing manifest.
	if f.readErr != nil {
		return nil, f.readErr
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Facts) readAll() error {
	dir := f.Dir
	var err error
	if f.Has("package.json") {
		if f.Workspace, err = findWorkspace(dir); err != nil {
			return err
		}
	}
	if f.Node, err = readNode(f); err != nil {
		return err
	}
	if f.Python, err = readPython(f); err != nil {
		return err
	}
	if f.Ruby, err = readRuby(f); err != nil {
		return err
	}
	if f.PHP, err = readPHP(f); err != nil {
		return err
	}
	if f.DotNet, err = readDotNet(f); err != nil {
		return err
	}
	if f.Rust, err = readRust(f); err != nil {
		return err
	}
	if f.Python != nil && f.Workspace == nil {
		if f.UvWorkspace, err = findUvWorkspace(dir); err != nil {
			return err
		}
	}
	if ws := f.UvWorkspace; ws != nil {
		if !fileExists(filepath.Join(ws.Root, "uv.lock")) {
			f.cantBuild("python", fmt.Errorf("the uv workspace at %s has no uv.lock; run uv lock there", ws.Root))
		}
		if f.Python.Name == "" {
			f.cantBuild("python", fmt.Errorf("%s/pyproject.toml has no project name, which uv needs to select it", ws.App))
		}
		f.Python.Manager = "uv"
		if v := f.firstLine(ws.Root, ".python-version"); v != "" && !f.Has(".python-version") {
			f.Python.Version = v
		}
	}
	if s, ok := f.read("go.mod"); ok {
		if f.Go, err = modfile.Parse("go.mod", []byte(s), nil); err != nil {
			return err
		}
		if f.GoCgo, err = goUsesCgo(f); err != nil {
			return err
		}
		if f.GoMains, err = goMains(f); err != nil {
			return err
		}
	}
	if s, ok := f.read("Procfile"); ok {
		f.Start = procfileWeb(s)
	}
	if s, ok := f.read("terril.toml"); ok {
		if f.Config, err = parseConfig(s); err != nil {
			return err
		}
	}
	if f.Config.Start != "" {
		f.Start = f.Config.Start
	}
	return nil
}

type Node struct {
	Package        PackageJSON
	Manager        string
	ManagerVersion string
	Lockfile       string
	Version        string
	PnP            bool
	// Production bin names from package-lock.json, mapped to their file under node_modules.
	Bins map[string]string
	// Where vite build writes, from vite.config. Empty when vite isn't a dependency.
	ViteOutDir string
}

type PackageJSON struct {
	Name            string            `json:"name"`
	Main            string            `json:"main"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"`
	Engines         struct {
		Node string `json:"node"`
	} `json:"engines"`
}

var nodeLockfiles = []struct{ file, manager string }{
	{"pnpm-lock.yaml", "pnpm"},
	{"package-lock.json", "npm"},
	{"yarn.lock", "yarn"},
	{"bun.lock", "bun"},
	{"bun.lockb", "bun"},
}

func readNode(f *Facts) (*Node, error) {
	s, ok := f.read("package.json")
	if !ok {
		return nil, nil
	}
	n := &Node{}
	if err := json.Unmarshal([]byte(s), &n.Package); err != nil {
		return nil, fmt.Errorf("package.json: %w", err)
	}
	for _, l := range nodeLockfiles {
		if f.rootHas(l.file) {
			n.Manager, n.Lockfile = l.manager, l.file
			break
		}
	}
	pm := n.Package.PackageManager
	if pm == "" && f.Workspace != nil {
		if rootPkg, ok := f.rootRead("package.json"); ok {
			var root PackageJSON
			if err := json.Unmarshal([]byte(rootPkg), &root); err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Join(f.root(), "package.json"), err)
			}
			pm = root.PackageManager
		}
	}
	// packageManager looks like "pnpm@10.4.1+sha512.abc".
	if pm != "" {
		name, version, _ := strings.Cut(strings.SplitN(pm, "+", 2)[0], "@")
		if n.Manager == "" {
			n.Manager = name
		}
		if name == n.Manager {
			n.ManagerVersion = version
		}
	}
	if n.Manager == "" {
		n.Manager = "npm"
	}
	if n.Lockfile == "package-lock.json" && f.Workspace == nil {
		lock, _ := f.read("package-lock.json")
		var err error
		if n.Bins, err = lockBins(lock); err != nil {
			return nil, err
		}
	}
	if n.Manager == "yarn" {
		lock, _ := f.rootRead("yarn.lock")
		major, _, _ := strings.Cut(n.ManagerVersion, ".")
		if f.rootHas(".yarnrc.yml") || strings.Contains(lock, "__metadata:") || major != "" && major != "1" {
			n.Manager = "yarn-berry"
			var yarnrc struct {
				NodeLinker string `yaml:"nodeLinker"`
			}
			if s, ok := f.rootRead(".yarnrc.yml"); ok {
				if err := yaml.Unmarshal([]byte(s), &yarnrc); err != nil {
					return nil, fmt.Errorf("%s: %w", filepath.Join(f.root(), ".yarnrc.yml"), err)
				}
			}
			n.PnP = yarnrc.NodeLinker != "node-modules" && yarnrc.NodeLinker != "pnpm"
		}
	}
	n.Version = strings.TrimPrefix(f.firstLine(f.Dir, ".nvmrc", ".node-version"), "v")
	if n.Version == "" && f.Workspace != nil {
		n.Version = strings.TrimPrefix(f.firstLine(f.Workspace.Root, ".nvmrc", ".node-version"), "v")
	}
	if n.Version == "" || strings.HasPrefix(n.Version, "lts") {
		n.Version = n.Package.Engines.Node
	}
	pkg := n.Package
	if pkg.Dependencies["vite"] != "" || pkg.DevDependencies["vite"] != "" {
		n.ViteOutDir = "dist"
		for _, name := range []string{"vite.config.ts", "vite.config.js", "vite.config.mts", "vite.config.mjs"} {
			src, ok := f.read(name)
			if !ok {
				continue
			}
			var dirs []string
			for _, m := range viteOutDirRe.FindAllStringSubmatch(jsCommentRe.ReplaceAllString(src, ""), -1) {
				if d := path.Clean(m[1]); !slices.Contains(dirs, d) {
					dirs = append(dirs, d)
				}
			}
			if len(dirs) > 1 {
				return nil, fmt.Errorf("%s sets outDir to %s; terril can't tell which one vite build writes", name, strings.Join(dirs, " and "))
			}
			if len(dirs) == 1 {
				n.ViteOutDir = dirs[0]
			}
			break
		}
	}
	return n, nil
}

var (
	viteOutDirRe = regexp.MustCompile(`outDir:\s*["']([^"']+)["']`)
	// Block comments, and line comments that start a line, so a URL in a string survives.
	jsCommentRe = regexp.MustCompile(`(?s:/\*.*?\*/)|(?m:^\s*//.*$)`)
)

func lockBins(lock string) (map[string]string, error) {
	var doc struct {
		Packages map[string]struct {
			Bin map[string]string `json:"bin"`
			Dev bool              `json:"dev"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(lock), &doc); err != nil {
		return nil, fmt.Errorf("package-lock.json: %w", err)
	}
	bins := map[string]string{}
	for key, pkg := range doc.Packages {
		dir, top := strings.CutPrefix(key, "node_modules/")
		if !top || pkg.Dev || strings.Contains(dir, "/node_modules/") {
			continue
		}
		for name, file := range pkg.Bin {
			bins[name] = path.Join(key, file)
		}
	}
	return bins, nil
}

// A JS or uv workspace the app belongs to. Root is the build context. Members is only set for
// a JS workspace.
type Workspace struct {
	Root    string
	App     string
	Members map[string]Member
	// Every member's folder, relative to Root and sorted.
	Dirs []string
}

type Member struct {
	Dir   string
	Deps  []string
	Build bool
}

// findWorkspace walks up from an app to the JS workspace that lists it, stopping at the repo root.
func findWorkspace(dir string) (*Workspace, error) {
	app, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for _, parent := range parentsInRepo(app) {
		patterns, err := workspacePatterns(parent)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(parent, app)
		rel = filepath.ToSlash(rel)
		if inWorkspace(patterns, rel) {
			members, err := readMembers(parent, patterns)
			var dirs []string
			for _, m := range members {
				dirs = append(dirs, m.Dir)
			}
			slices.Sort(dirs)
			return &Workspace{Root: parent, App: rel, Members: members, Dirs: dirs}, err
		}
	}
	return nil, nil
}

// parentsInRepo lists the folders above app up to the root of its git repository. An app that is
// itself a repository root has none.
func parentsInRepo(app string) []string {
	var dirs []string
	for dir := app; !fileExists(filepath.Join(dir, ".git")) && dir != filepath.Dir(dir); {
		dir = filepath.Dir(dir)
		dirs = append(dirs, dir)
	}
	return dirs
}

func validPatterns(file string, patterns []string) error {
	for _, p := range patterns {
		if !doublestar.ValidatePattern(strings.TrimPrefix(p, "!")) {
			return fmt.Errorf("%s: %q isn't a valid folder pattern", file, p)
		}
	}
	return nil
}

func workspacePatterns(dir string) ([]string, error) {
	if b, err := os.ReadFile(filepath.Join(dir, "pnpm-workspace.yaml")); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			return nil, err
		}
		var ws struct {
			Packages []string `yaml:"packages"`
		}
		if err := yaml.Unmarshal(b, &ws); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, "pnpm-workspace.yaml"), err)
		}
		return ws.Packages, validPatterns(filepath.Join(dir, "pnpm-workspace.yaml"), ws.Packages)
	}
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var pkg struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, "package.json"), err)
	}
	if len(pkg.Workspaces) == 0 {
		return nil, nil
	}
	var patterns []string
	if json.Unmarshal(pkg.Workspaces, &patterns) != nil {
		var yarnClassic struct {
			Packages []string `json:"packages"`
		}
		if err := json.Unmarshal(pkg.Workspaces, &yarnClassic); err != nil {
			return nil, fmt.Errorf("%s: workspaces must be a list or have a packages list", filepath.Join(dir, "package.json"))
		}
		patterns = yarnClassic.Packages
	}
	return patterns, validPatterns(filepath.Join(dir, "package.json"), patterns)
}

// A later pattern wins, so a pnpm "!" pattern takes back a folder an earlier one listed.
func inWorkspace(patterns []string, rel string) bool {
	in := false
	for _, p := range patterns {
		p, exclude := strings.CutPrefix(p, "!")
		if ok, _ := doublestar.Match(path.Clean(p), rel); ok {
			in = !exclude
		}
	}
	return in
}

func readMembers(root string, patterns []string) (map[string]Member, error) {
	members := map[string]Member{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if d.Name() != "package.json" || p == filepath.Join(root, "package.json") {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(p))
		rel = filepath.ToSlash(rel)
		if !inWorkspace(patterns, rel) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var pkg struct {
			Name            string            `json:"name"`
			Scripts         map[string]string `json:"scripts"`
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if err := json.Unmarshal(b, &pkg); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		m := Member{Dir: rel, Build: pkg.Scripts["build"] != ""}
		for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
			for name := range deps {
				m.Deps = append(m.Deps, name)
			}
		}
		slices.Sort(m.Deps)
		// Nothing can depend on a member without a name, so only the app itself needs one.
		if pkg.Name == "" {
			return nil
		}
		if other, ok := members[pkg.Name]; ok {
			return fmt.Errorf("%s and %s are both named %s; workspace tools need one name per member", other.Dir, rel, pkg.Name)
		}
		members[pkg.Name] = m
		return nil
	})
	return members, err
}

type Python struct {
	Name    string
	Manager string
	// requirements.txt and the files it pulls in with -r or -c, for the install step to copy.
	RequirementFiles []string
	Requirements     []string
	Deps             map[string]bool
	Version          string
	// main.py, app.py and wsgi.py, in that order, when the app has them.
	Entries []string
	// The module:variable that first assigns each call at the top of an entry file, such as
	// "FastAPI": "main:app" for app = FastAPI() in main.py.
	Apps map[string]string
	// The package manage.py's DJANGO_SETTINGS_MODULE names, such as mysite.
	DjangoProject string
	// The Django project's settings.py sets STATIC_ROOT, which collectstatic needs.
	DjangoStaticRoot bool
}

func readPython(f *Facts) (*Python, error) {
	py := &Python{Manager: "pip", Deps: map[string]bool{}, Apps: map[string]string{}}
	found := false
	if f.Has("requirements.txt") {
		found = true
		if err := f.readRequirements(py, "requirements.txt", true); err != nil {
			return nil, err
		}
	}
	if s, ok := f.read("pyproject.toml"); ok {
		found = true
		var doc struct {
			Project struct {
				Name           string   `toml:"name"`
				Dependencies   []string `toml:"dependencies"`
				RequiresPython string   `toml:"requires-python"`
			} `toml:"project"`
			Tool struct {
				Poetry struct {
					Dependencies map[string]any `toml:"dependencies"`
				} `toml:"poetry"`
			} `toml:"tool"`
		}
		if _, err := toml.Decode(s, &doc); err != nil {
			return nil, fmt.Errorf("pyproject.toml: %w", err)
		}
		py.Name = doc.Project.Name
		py.Requirements = doc.Project.Dependencies
		for _, spec := range doc.Project.Dependencies {
			py.Deps[distName(spec)] = true
		}
		for name := range doc.Tool.Poetry.Dependencies {
			if name != "python" {
				py.Deps[distName(name)] = true
			}
		}
		py.Version = doc.Project.RequiresPython
		// Poetry's own table holds the Python requirement when [project] has none.
		if v, ok := doc.Tool.Poetry.Dependencies["python"].(string); ok && py.Version == "" {
			py.Version = v
		}
	}
	if !found {
		return nil, nil
	}
	switch {
	case f.Has("uv.lock"):
		py.Manager = "uv"
	case f.Has("poetry.lock"):
		py.Manager = "poetry"
	}
	if v := f.firstLine(f.Dir, ".python-version"); v != "" {
		py.Version = v
	}
	for _, file := range []string{"main.py", "app.py", "wsgi.py"} {
		if !f.Has(file) {
			continue
		}
		py.Entries = append(py.Entries, file)
		src, _ := f.read(file)
		for _, m := range assignCallRe.FindAllStringSubmatch(src, -1) {
			if _, seen := py.Apps[m[2]]; !seen {
				py.Apps[m[2]] = strings.TrimSuffix(file, ".py") + ":" + m[1]
			}
		}
	}
	manage, _ := f.read("manage.py")
	if m := djangoSettingsRe.FindStringSubmatch(manage); m != nil {
		py.DjangoProject, _, _ = strings.Cut(m[1], ".")
		file := strings.ReplaceAll(m[1], ".", "/")
		settings, ok := f.read(file + ".py")
		if !ok {
			settings, _ = f.read(file + "/__init__.py")
		}
		py.DjangoStaticRoot = staticRootRe.MatchString(settings)
	}
	return py, nil
}

var (
	assignCallRe     = regexp.MustCompile(`(?m)^(\w+)\s*=\s*(\w+)\(`)
	djangoSettingsRe = regexp.MustCompile(`DJANGO_SETTINGS_MODULE["']\s*,\s*["'](\w+(?:\.\w+)*\.settings(?:\.\w+)*)["']`)
	staticRootRe     = regexp.MustCompile(`(?m)^STATIC_ROOT\s*=`)
	distNameRe       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*`)
	normalizeRe      = regexp.MustCompile(`[-_.]+`)
)

// PEP 503 normalization, so "Django_Rest" matches "django-rest".
func distName(spec string) string {
	return strings.ToLower(normalizeRe.ReplaceAllString(distNameRe.FindString(strings.TrimSpace(spec)), "-"))
}

// readRequirements adds the deps of a requirements file and of the files it includes with -r.
// A -c file only pins versions, so it is copied but adds no deps.
func (f *Facts) readRequirements(py *Python, file string, deps bool) error {
	if slices.Contains(py.RequirementFiles, file) {
		return nil
	}
	s, _ := f.read(file)
	py.RequirementFiles = append(py.RequirementFiles, file)
	for _, line := range strings.Split(strings.ReplaceAll(s, "\\\n", ""), "\n") {
		line = strings.TrimSpace(requirementCommentRe.ReplaceAllString(line, ""))
		if m := includeRe.FindStringSubmatch(line); m != nil {
			include := path.Join(path.Dir(file), m[2])
			if !f.Has(include) {
				return fmt.Errorf("%s includes %s, which isn't in the app", file, include)
			}
			if err := f.readRequirements(py, include, deps && (m[1] == "-r" || m[1] == "--requirement")); err != nil {
				return err
			}
		} else if name := distName(line); deps && name != "" {
			py.Deps[name] = true
		}
	}
	return nil
}

var (
	requirementCommentRe = regexp.MustCompile(`(^|\s)#.*$`)
	includeRe            = regexp.MustCompile(`^(-r|--requirement|-c|--constraint)[\s=]*(\S+)$`)
)

// findUvWorkspace walks up from an app to the uv workspace that lists it, stopping at the repo root.
func findUvWorkspace(dir string) (*Workspace, error) {
	app, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for _, parent := range parentsInRepo(app) {
		b, err := os.ReadFile(filepath.Join(parent, "pyproject.toml"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			var doc struct {
				Tool struct {
					Uv struct {
						Workspace struct {
							Members []string `toml:"members"`
							Exclude []string `toml:"exclude"`
						} `toml:"workspace"`
					} `toml:"uv"`
				} `toml:"tool"`
			}
			if _, err := toml.Decode(string(b), &doc); err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Join(parent, "pyproject.toml"), err)
			}
			ws := doc.Tool.Uv.Workspace
			if err := validPatterns(filepath.Join(parent, "pyproject.toml"), slices.Concat(ws.Members, ws.Exclude)); err != nil {
				return nil, err
			}
			rel, _ := filepath.Rel(parent, app)
			rel = filepath.ToSlash(rel)
			if inWorkspace(ws.Members, rel) && !inWorkspace(ws.Exclude, rel) {
				dirs, err := uvMemberDirs(parent, ws.Members, ws.Exclude)
				return &Workspace{Root: parent, App: rel, Dirs: dirs}, err
			}
		}
	}
	return nil, nil
}

func uvMemberDirs(root string, members, exclude []string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") && p != root || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.Name() != "pyproject.toml" || p == filepath.Join(root, "pyproject.toml") {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(p))
		rel = filepath.ToSlash(rel)
		if inWorkspace(members, rel) && !inWorkspace(exclude, rel) {
			dirs = append(dirs, rel)
		}
		return nil
	})
	slices.Sort(dirs)
	return dirs, err
}

type Ruby struct {
	Version string
	// Gem names from Gemfile.lock, including dependencies of dependencies.
	Gems map[string]bool
}

var (
	gemfileRubyRe     = regexp.MustCompile(`(?m)^\s*ruby\s+["']([^"']+)["']`)
	rubyVersionNumber = regexp.MustCompile(`\d+(\.\d+){0,2}`)
)

// gemfileLock holds each top-level section of a Gemfile.lock, such as PLATFORMS, as its
// indented lines.
type gemfileLock map[string][]string

func parseGemfileLock(s string) gemfileLock {
	lock := gemfileLock{}
	section := ""
	for _, line := range strings.Split(s, "\n") {
		switch {
		case line == "":
		case !strings.HasPrefix(line, " "):
			section = strings.TrimSpace(line)
			lock[section] = nil
		default:
			lock[section] = append(lock[section], line)
		}
	}
	return lock
}

// gems lists the specs of every source section, including dependencies of dependencies.
func (l gemfileLock) gems() map[string]bool {
	gems := map[string]bool{}
	for _, section := range []string{"GEM", "GIT", "PATH"} {
		for _, line := range l[section] {
			if name, _, ok := strings.Cut(line, " ("); ok && strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "     ") {
				gems[strings.TrimSpace(name)] = true
			}
		}
	}
	return gems
}

func (l gemfileLock) fields(section string) []string {
	var fields []string
	for _, line := range l[section] {
		fields = append(fields, strings.Fields(line)...)
	}
	return fields
}

func readRuby(f *Facts) (*Ruby, error) {
	if !f.Has("Gemfile") {
		return nil, nil
	}
	lock, ok := f.read("Gemfile.lock")
	if !ok {
		f.cantBuild("ruby", fmt.Errorf("no Gemfile.lock next to the Gemfile; run bundle lock and commit it"))
		return &Ruby{}, nil
	}
	parsed := parseGemfileLock(lock)
	r := &Ruby{Gems: parsed.gems()}
	platforms := parsed.fields("PLATFORMS")
	switch {
	case len(platforms) == 0:
		f.cantBuild("ruby", fmt.Errorf("the Gemfile.lock lists no platforms; run bundle lock --add-platform x86_64-linux aarch64-linux"))
	case !slices.ContainsFunc(platforms, func(p string) bool { return p == "ruby" || strings.Contains(p, "-linux") }):
		f.cantBuild("ruby", fmt.Errorf("the Gemfile.lock only lists %s; run bundle lock --add-platform x86_64-linux aarch64-linux",
			strings.Join(platforms, ", ")))
	}
	if v := f.firstLine(f.Dir, ".ruby-version"); v != "" {
		r.Version = rubyVersionNumber.FindString(v)
	} else if v := parsed.fields("RUBY VERSION"); len(v) > 1 {
		r.Version = rubyVersionNumber.FindString(v[1])
	} else {
		gemfile, _ := f.read("Gemfile")
		if m := gemfileRubyRe.FindStringSubmatch(gemfile); m != nil {
			r.Version = m[1]
		}
	}
	return r, nil
}

type PHP struct {
	Composer bool
	// Package names from composer.lock.
	Packages map[string]bool
	// "public" for apps with public/index.php, "" for an index.php at the root.
	DocRoot string
}

func readPHP(f *Facts) (*PHP, error) {
	p := &PHP{Packages: map[string]bool{}, Composer: f.Has("composer.json")}
	switch {
	case f.Has("public/index.php"):
		p.DocRoot = "public"
	case f.Has("index.php"):
	case p.Composer:
		f.cantBuild("php", fmt.Errorf("composer.json found, but no index.php in public/ or the app root"))
		return p, nil
	default:
		return nil, nil
	}
	if !p.Composer {
		return p, nil
	}
	lock, ok := f.read("composer.lock")
	if !ok {
		f.cantBuild("php", fmt.Errorf("composer.json has no composer.lock; run composer update and commit it"))
		return p, nil
	}
	var doc struct {
		Packages []struct {
			Name string `json:"name"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(lock), &doc); err != nil {
		return nil, fmt.Errorf("composer.lock: %w", err)
	}
	for _, pkg := range doc.Packages {
		p.Packages[pkg.Name] = true
	}
	return p, nil
}

type DotNet struct {
	// The project to publish, relative to the app.
	Project string
	// Every project file, relative to the app, so restore sees the projects it references.
	Projects        []string
	Assembly        string
	TargetFramework string
	Web             bool
	// The project turns off culture data, so the runtime can leave ICU out.
	Invariant bool
	// From global.json, when it pins one.
	SDKVersion string
}

type csproj struct {
	SDK    string `xml:"Sdk,attr"`
	Groups []struct {
		TargetFramework        string `xml:"TargetFramework"`
		TargetFrameworks       string `xml:"TargetFrameworks"`
		AssemblyName           string `xml:"AssemblyName"`
		InvariantGlobalization string `xml:"InvariantGlobalization"`
	} `xml:"PropertyGroup"`
}

func readDotNet(f *Facts) (*DotNet, error) {
	var projects []string
	err := filepath.WalkDir(f.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != f.Dir && (d.Name() == "bin" || d.Name() == "obj" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if strings.HasSuffix(d.Name(), ".csproj") {
			rel, _ := filepath.Rel(f.Dir, p)
			projects = append(projects, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil || len(projects) == 0 {
		return nil, err
	}
	slices.Sort(projects)

	parsed := map[string]csproj{}
	var web []string
	for _, p := range projects {
		s, _ := f.read(p)
		var c csproj
		if err := xml.Unmarshal([]byte(s), &c); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		parsed[p] = c
		if c.SDK == "Microsoft.NET.Sdk.Web" {
			web = append(web, p)
		}
	}
	// The project at the app root wins. Otherwise the only web project does, so a solution with
	// one API and its test projects builds the API.
	atRoot := slices.DeleteFunc(slices.Clone(projects), func(p string) bool { return strings.Contains(p, "/") })
	var project string
	switch {
	case len(atRoot) > 1:
		f.cantBuild("dotnet", fmt.Errorf("several .NET projects at the app root (%s); keep one", strings.Join(atRoot, ", ")))
		return &DotNet{}, nil
	case len(atRoot) == 1:
		project = atRoot[0]
	case len(web) == 1:
		project = web[0]
	case len(web) > 1:
		f.cantBuild("dotnet", fmt.Errorf("several .NET web projects (%s); point terril at the folder of the one to build", strings.Join(web, ", ")))
		return &DotNet{}, nil
	case len(projects) == 1:
		project = projects[0]
	default:
		f.cantBuild("dotnet", fmt.Errorf("several .NET projects (%s); point terril at the folder of the one to build", strings.Join(projects, ", ")))
		return &DotNet{}, nil
	}

	c := parsed[project]
	d := &DotNet{
		Project:  project,
		Projects: projects,
		Assembly: strings.TrimSuffix(path.Base(project), ".csproj"),
		Web:      c.SDK == "Microsoft.NET.Sdk.Web",
	}
	for _, g := range c.Groups {
		if g.AssemblyName != "" {
			d.Assembly = g.AssemblyName
		}
		if strings.EqualFold(g.InvariantGlobalization, "true") {
			d.Invariant = true
		}
		if g.TargetFramework != "" {
			d.TargetFramework = g.TargetFramework
		} else if g.TargetFrameworks != "" && d.TargetFramework == "" {
			frameworks := strings.Split(g.TargetFrameworks, ";")
			d.TargetFramework = strings.TrimSpace(frameworks[len(frameworks)-1])
		}
	}
	if s, ok := f.read("global.json"); ok {
		var g struct {
			SDK struct {
				Version string `json:"version"`
			} `json:"sdk"`
		}
		if err := json.Unmarshal([]byte(s), &g); err != nil {
			return nil, fmt.Errorf("global.json: %w", err)
		}
		d.SDKVersion = g.SDK.Version
	}
	return d, nil
}

type Rust struct {
	Package string
	Bin     string
	// The toolchain file, rust-toolchain.toml or rust-toolchain, when the app has one.
	Toolchain string
}

type cargoToml struct {
	Package *struct {
		Name       string `toml:"name"`
		DefaultRun string `toml:"default-run"`
		Autobins   *bool  `toml:"autobins"`
	} `toml:"package"`
	Bin []struct {
		Name string `toml:"name"`
		Path string `toml:"path"`
	} `toml:"bin"`
	Workspace *struct {
		Members []string `toml:"members"`
	} `toml:"workspace"`
}

func readRust(f *Facts) (*Rust, error) {
	root, ok := f.read("Cargo.toml")
	if !ok {
		return nil, nil
	}
	var manifest cargoToml
	if _, err := toml.Decode(root, &manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(f.Dir, "Cargo.toml"), err)
	}
	if !f.Has("Cargo.lock") {
		f.cantBuild("rust", fmt.Errorf("no Cargo.lock next to Cargo.toml; run cargo generate-lockfile and commit it"))
		return &Rust{}, nil
	}
	if manifest.Package != nil && manifest.Package.DefaultRun != "" {
		return &Rust{Package: manifest.Package.Name, Bin: manifest.Package.DefaultRun, Toolchain: rustToolchain(f)}, nil
	}
	dirs := []string{"."}
	if manifest.Workspace != nil {
		for _, pattern := range manifest.Workspace.Members {
			matches, err := filepath.Glob(filepath.Join(f.Dir, pattern, "Cargo.toml"))
			if err != nil {
				return nil, fmt.Errorf("%s: workspace member %q: %w", filepath.Join(f.Dir, "Cargo.toml"), pattern, err)
			}
			for _, m := range matches {
				rel, _ := filepath.Rel(f.Dir, filepath.Dir(m))
				if rel = filepath.ToSlash(rel); !slices.Contains(dirs, rel) {
					dirs = append(dirs, rel)
				}
			}
		}
	}

	type bin struct{ pkg, name string }
	var bins []bin
	for _, dir := range dirs {
		var c cargoToml
		if dir == "." {
			c = manifest
		} else {
			s, _ := f.read(path.Join(dir, "Cargo.toml"))
			if _, err := toml.Decode(s, &c); err != nil {
				return nil, fmt.Errorf("%s/Cargo.toml: %w", dir, err)
			}
		}
		if c.Package == nil {
			continue
		}
		names := map[string]bool{}
		mainNamed := false
		for _, b := range c.Bin {
			names[b.Name] = true
			mainNamed = mainNamed || path.Clean(b.Path) == "src/main.rs"
		}
		// Cargo finds binaries in src/main.rs and src/bin/ unless autobins is off.
		if c.Package.Autobins == nil || *c.Package.Autobins {
			if f.Has(path.Join(dir, "src/main.rs")) && !mainNamed {
				names[c.Package.Name] = true
			}
			entries, _ := os.ReadDir(filepath.Join(f.Dir, dir, "src/bin"))
			for _, e := range entries {
				if name, ok := strings.CutSuffix(e.Name(), ".rs"); ok && !e.IsDir() {
					names[name] = true
				} else if e.IsDir() && f.Has(path.Join(dir, "src/bin", e.Name(), "main.rs")) {
					names[e.Name()] = true
				}
			}
		}
		for name := range names {
			bins = append(bins, bin{c.Package.Name, name})
		}
	}
	switch len(bins) {
	case 0:
		f.cantBuild("rust", fmt.Errorf("no Rust binary: add src/main.rs or a [[bin]] to Cargo.toml"))
		return &Rust{}, nil
	case 1:
		return &Rust{Package: bins[0].pkg, Bin: bins[0].name, Toolchain: rustToolchain(f)}, nil
	}
	var names []string
	for _, b := range bins {
		names = append(names, b.name)
	}
	if manifest.Package == nil {
		names = names[:0]
		for _, b := range bins {
			names = append(names, b.pkg+"/"+b.name)
		}
		slices.Sort(names)
		f.cantBuild("rust", fmt.Errorf("several Rust binaries (%s); set default-run in the [package] of the one to build", strings.Join(names, ", ")))
		return &Rust{}, nil
	}
	slices.Sort(names)
	f.cantBuild("rust", fmt.Errorf("several Rust binaries (%s); set default-run in Cargo.toml", strings.Join(names, ", ")))
	return &Rust{}, nil
}

func rustToolchain(f *Facts) string {
	for _, name := range []string{"rust-toolchain.toml", "rust-toolchain"} {
		if f.Has(name) {
			return name
		}
	}
	return ""
}

// Modules that need cgo. A miss fails the build with the package's name, so it can be added here.
var cgoModules = []string{
	"github.com/mattn/go-sqlite3",
	"github.com/confluentinc/confluent-kafka-go",
	"github.com/confluentinc/confluent-kafka-go/v2",
}

func goUsesCgo(f *Facts) (bool, error) {
	for _, r := range f.Go.Require {
		if slices.Contains(cgoModules, r.Mod.Path) {
			return true, nil
		}
	}
	found := false
	err := filepath.WalkDir(f.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != f.Dir && (name == "vendor" || name == "testdata" || name == "node_modules" || strings.HasPrefix(name, ".") || fileExists(filepath.Join(p, "go.mod"))) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		found = slices.ContainsFunc(file.Imports, func(i *ast.ImportSpec) bool { return i.Path.Value == `"C"` })
		return nil
	})
	return found, err
}

func goMains(f *Facts) ([]string, error) {
	if f.Has("main.go") {
		return []string{"."}, nil
	}
	files, err := filepath.Glob(filepath.Join(f.Dir, "cmd", "*", "main.go"))
	var mains []string
	for _, file := range files {
		rel, _ := filepath.Rel(f.Dir, filepath.Dir(file))
		mains = append(mains, "./"+filepath.ToSlash(rel))
	}
	return mains, err
}

func procfileWeb(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if cmd, ok := strings.CutPrefix(strings.TrimSpace(line), "web:"); ok {
			return strings.TrimSpace(cmd)
		}
	}
	return ""
}

type Config struct {
	Provider      string   `toml:"provider"`
	Start         string   `toml:"start"`
	Packages      []string `toml:"packages"`
	BuildPackages []string `toml:"build-packages"`
}

var aptPackageRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)

func parseConfig(s string) (Config, error) {
	var c Config
	md, err := toml.Decode(s, &c)
	if err != nil {
		return c, fmt.Errorf("terril.toml: %w", err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return c, fmt.Errorf("terril.toml: unknown key %q (have: provider, start, packages, build-packages)", keys[0].String())
	}
	for _, p := range slices.Concat(c.Packages, c.BuildPackages) {
		if !aptPackageRe.MatchString(p) {
			return c, fmt.Errorf("terril.toml: %q isn't a Debian package name", p)
		}
	}
	return c, nil
}
