package pack

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

type Facts struct {
	Dir       string
	Workspace *Workspace
	Node      *Node
	Python    *Python
	Go        *modfile.File
	Config    Config
	// From wolkpack.toml, else the Procfile web: line.
	Start string
}

// A JS workspace the app belongs to. Root is the build context.
type Workspace struct {
	Root    string
	App     string
	Members map[string]Member
}

type Member struct {
	Dir   string
	Deps  []string
	Build bool
}

type Config struct {
	Provider      string   `toml:"provider"`
	Start         string   `toml:"start"`
	Packages      []string `toml:"packages"`
	BuildPackages []string `toml:"build-packages"`
}

type Node struct {
	Package        PackageJSON
	Manager        string
	ManagerVersion string
	Lockfile       string
	Version        string
	PnP            bool
}

type PackageJSON struct {
	Name           string            `json:"name"`
	Main           string            `json:"main"`
	Scripts        map[string]string `json:"scripts"`
	PackageManager string            `json:"packageManager"`
	Engines        struct {
		Node string `json:"node"`
	} `json:"engines"`
}

type Python struct {
	Manager      string
	Requirements []string
	Deps         map[string]bool
	Version      string
}

func (f *Facts) Has(name string) bool {
	_, err := os.Stat(filepath.Join(f.Dir, name))
	return err == nil
}

func (f *Facts) read(name string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(f.Dir, name))
	return string(b), err == nil
}

func (f *Facts) root() string {
	if f.Workspace != nil {
		return f.Workspace.Root
	}
	return f.Dir
}

func (f *Facts) rootHas(name string) bool {
	_, err := os.Stat(filepath.Join(f.root(), name))
	return err == nil
}

func (f *Facts) rootRead(name string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(f.root(), name))
	return string(b), err == nil
}

func (f *Facts) firstLine(names ...string) string {
	for _, n := range names {
		if s, ok := f.read(n); ok {
			return strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
		}
	}
	return ""
}

func Read(dir string) (*Facts, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	f := &Facts{Dir: dir}
	var err error
	if f.Has("package.json") {
		if f.Workspace, err = findWorkspace(dir); err != nil {
			return nil, err
		}
	}
	if f.Node, err = readNode(f); err != nil {
		return nil, err
	}
	if f.Python, err = readPython(f); err != nil {
		return nil, err
	}
	if s, ok := f.read("go.mod"); ok {
		if f.Go, err = modfile.ParseLax("go.mod", []byte(s), nil); err != nil {
			return nil, err
		}
	}
	if s, ok := f.read("Procfile"); ok {
		f.Start = procfileWeb(s)
	}
	if s, ok := f.read("wolkpack.toml"); ok {
		if f.Config, err = parseConfig(s); err != nil {
			return nil, err
		}
	}
	if f.Config.Start != "" {
		f.Start = f.Config.Start
	}
	return f, nil
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
	if rootPkg, ok := f.rootRead("package.json"); ok && pm == "" && f.Workspace != nil {
		var root PackageJSON
		if err := json.Unmarshal([]byte(rootPkg), &root); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(f.root(), "package.json"), err)
		}
		pm = root.PackageManager
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
	if n.Manager == "yarn" {
		lock, _ := f.rootRead("yarn.lock")
		yarnrc, _ := f.rootRead(".yarnrc.yml")
		if f.rootHas(".yarnrc.yml") || strings.Contains(lock, "__metadata:") || !strings.HasPrefix(n.ManagerVersion+"1", "1") {
			n.Manager = "yarn-berry"
			n.PnP = !nodeLinkerRe.MatchString(yarnrc)
		}
	}
	n.Version = strings.TrimPrefix(f.firstLine(".nvmrc", ".node-version"), "v")
	if n.Version == "" && f.Workspace != nil {
		root := &Facts{Dir: f.Workspace.Root}
		n.Version = strings.TrimPrefix(root.firstLine(".nvmrc", ".node-version"), "v")
	}
	if n.Version == "" || strings.HasPrefix(n.Version, "lts") {
		n.Version = n.Package.Engines.Node
	}
	return n, nil
}

// findWorkspace walks up from an app to the JS workspace that lists it, stopping at the repo root.
func findWorkspace(dir string) (*Workspace, error) {
	app, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for parent := filepath.Dir(app); ; parent = filepath.Dir(parent) {
		patterns, err := workspacePatterns(parent)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(parent, app)
		rel = filepath.ToSlash(rel)
		if inWorkspace(patterns, rel) {
			members, err := readMembers(parent, patterns)
			return &Workspace{Root: parent, App: rel, Members: members}, err
		}
		if _, err := os.Stat(filepath.Join(parent, ".git")); err == nil || parent == filepath.Dir(parent) {
			return nil, nil
		}
	}
}

func inWorkspace(patterns []string, rel string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, rel); ok || strings.HasSuffix(p, "/**") && strings.HasPrefix(rel, strings.TrimSuffix(p, "**")) {
			return true
		}
	}
	return false
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
		members[pkg.Name] = m
		return nil
	})
	return members, err
}

func workspacePatterns(dir string) ([]string, error) {
	if b, err := os.ReadFile(filepath.Join(dir, "pnpm-workspace.yaml")); err == nil {
		var ws struct {
			Packages []string `yaml:"packages"`
		}
		if err := yaml.Unmarshal(b, &ws); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, "pnpm-workspace.yaml"), err)
		}
		return ws.Packages, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, nil
	}
	var pkg struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil || len(pkg.Workspaces) == 0 {
		return nil, nil
	}
	var patterns []string
	if json.Unmarshal(pkg.Workspaces, &patterns) != nil {
		var yarnClassic struct {
			Packages []string `json:"packages"`
		}
		json.Unmarshal(pkg.Workspaces, &yarnClassic)
		patterns = yarnClassic.Packages
	}
	return patterns, nil
}

var nodeLinkerRe = regexp.MustCompile(`(?m)^nodeLinker:\s*["']?(node-modules|pnpm)`)

func readPython(f *Facts) (*Python, error) {
	py := &Python{Manager: "pip", Deps: map[string]bool{}}
	found := false
	if s, ok := f.read("requirements.txt"); ok {
		found = true
		for _, name := range requirementNames(s) {
			py.Deps[name] = true
		}
	}
	if s, ok := f.read("pyproject.toml"); ok {
		found = true
		var doc struct {
			Project struct {
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
		py.Requirements = doc.Project.Dependencies
		for _, spec := range doc.Project.Dependencies {
			py.Deps[distName(spec)] = true
		}
		for name := range doc.Tool.Poetry.Dependencies {
			py.Deps[distName(name)] = true
		}
		py.Version = doc.Project.RequiresPython
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
	if v := f.firstLine(".python-version"); v != "" {
		py.Version = v
	}
	return py, nil
}

var distNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*`)
var normalizeRe = regexp.MustCompile(`[-_.]+`)

// PEP 503 normalization, so "Django_Rest" matches "django-rest".
func distName(spec string) string {
	return strings.ToLower(normalizeRe.ReplaceAllString(distNameRe.FindString(strings.TrimSpace(spec)), "-"))
}

func requirementNames(s string) []string {
	var names []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		if name := distName(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

var aptPackageRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)

func parseConfig(s string) (Config, error) {
	var c Config
	md, err := toml.Decode(s, &c)
	if err != nil {
		return c, fmt.Errorf("wolkpack.toml: %w", err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return c, fmt.Errorf("wolkpack.toml: unknown key %q (have: provider, start, packages, build-packages)", keys[0].String())
	}
	for _, p := range append(c.Packages, c.BuildPackages...) {
		if !aptPackageRe.MatchString(p) {
			return c, fmt.Errorf("wolkpack.toml: %q isn't a Debian package name", p)
		}
	}
	return c, nil
}

// Distroless images have no shell for a custom start command and no apt for packages.
func (f *Facts) needsDebianRuntime() bool {
	return f.Start != "" || len(f.Config.Packages) > 0
}

func procfileWeb(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if cmd, ok := strings.CutPrefix(strings.TrimSpace(line), "web:"); ok {
			return strings.TrimSpace(cmd)
		}
	}
	return ""
}
