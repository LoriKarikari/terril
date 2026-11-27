package pack

import (
	"fmt"
	"strings"
)

var pythonProvider = provider{
	name:     "python",
	manifest: "requirements.txt or pyproject.toml",
	detect: func(f *Facts) Role {
		if f.Python == nil {
			return 0
		}
		return Primary
	},
	plan:    planPython,
	runtime: &runtime{versions: &pythonVersions, version: pythonVersion},
}

const venv = "/app/.venv"

const pythonDir = "/python"

// pin: datasource=docker depName=ghcr.io/astral-sh/uv
const uvVersion = "0.13.5"

// pin: datasource=pypi depName=poetry
const poetryVersion = "2.5.1"

// Parts of uv's Python a server never uses. The bin/python3.x executable embeds the
// interpreter, so libpython is only for programs that embed Python themselves.
var pythonRuntimeExcludes = []string{
	"*/lib/libpython3*",
	"*/lib/python3*/site-packages/pip*",
	"*/lib/python3*/ensurepip",
	"*/lib/python3*/idlelib",
	"*/lib/python3*/tkinter",
	"*/lib/python3*/turtledemo",
	"*/lib/python3*/lib-dynload/_tkinter*",
	"*/lib/python3*/config-3*",
	"*/lib/tcl*",
	"*/lib/tk*",
	"*/lib/libtcl*",
	"*/lib/libtk*",
	"*/lib/itcl*",
	"*/lib/thread*",
	"*/include",
	"*/share",
	"*/bin/idle*",
	"*/bin/pip*",
}

func pythonVersion(f *Facts) (string, error) {
	return pythonVersions.pick(f.Python.Version, f.Today)
}

var venvPath = Env{"PATH", venv + "/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}

func planPython(f *Facts, assets []string) ([]Stage, Runtime, error) {
	py := f.Python
	v, err := pythonVersion(f)
	if err != nil {
		return nil, Runtime{}, err
	}
	uvCache := caches("/root/.cache/uv")
	steps := []Step{
		venvPath,
		Copy{From: fromImage("ghcr.io/astral-sh/uv:" + uvVersion), Src: []string{"/uv"}, Dst: "/usr/local/bin/uv"},
		Env{"UV_PYTHON", v},
		Env{"UV_PROJECT_ENVIRONMENT", venv},
		Env{"VIRTUAL_ENV", venv},
		Env{"UV_LINK_MODE", "copy"},
		Env{"UV_COMPILE_BYTECODE", "1"},
	}
	// uv's standalone Python needs only glibc, so the runtime can be distroless. The venv
	// links to /python by absolute path, so both stages keep it there. A shell or apt packages
	// need Debian, where python:slim's Python is smaller because it shares Debian's libraries.
	debian := f.needsDebianRuntime()
	buildImage, runtimeImage := "debian:trixie-slim", "gcr.io/distroless/cc-debian13"
	if debian {
		buildImage = "python:" + v + "-slim"
		runtimeImage = buildImage
		steps = append(steps, Env{"UV_PYTHON_PREFERENCE", "only-system"})
	} else {
		steps = append(steps,
			Env{"UV_PYTHON_INSTALL_DIR", pythonDir},
			Env{"UV_PYTHON_CACHE_DIR", "/root/.cache/uv/python"},
			Env{"UV_PYTHON_PREFERENCE", "only-managed"},
			Run{Cmd: "uv python install " + v, Caches: uvCache},
		)
	}
	install, ok := pythonInstalls[py.Manager]
	if !ok {
		return nil, Runtime{}, fmt.Errorf("terril has no install steps for %s", py.Manager)
	}
	steps = append(steps, install(f, uvCache)...)
	steps = append(steps, copyAssets(assets)...)
	// collectstatic exits with an error when STATIC_ROOT is unset.
	if py.Deps["django"] && py.DjangoStaticRoot {
		cmd := "python manage.py collectstatic --noinput"
		if ws := f.UvWorkspace; ws != nil {
			cmd = "cd " + shellQuote(ws.App) + " && " + cmd
		}
		steps = append(steps, Run{Cmd: cmd})
	}

	start, err := pythonStart(py)
	if err != nil && f.Start == "" {
		return nil, Runtime{}, err
	}
	rt := Runtime{
		From:   runtimeImage,
		Copies: []Copy{{From: fromStage(buildStage), Src: []string{"/app/"}, Dst: "/app/"}},
		Env:    []Env{venvPath, {"PYTHONUNBUFFERED", "1"}},
		Cmd:    start,
	}
	if !debian {
		rt.Copies = append([]Copy{{From: fromStage(buildStage), Src: []string{pythonDir + "/"}, Dst: pythonDir + "/", RootOwned: true, Exclude: pythonRuntimeExcludes}}, rt.Copies...)
		rt.Env = append(rt.Env, Env{"SSL_CERT_FILE", "/etc/ssl/certs/ca-certificates.crt"})
	}
	if ws := f.UvWorkspace; ws != nil {
		rt.Workdir = "/app/" + ws.App
	}
	return []Stage{{Name: buildStage, From: fromImage(buildImage), Steps: steps}}, rt, nil
}

// Each install ends with the source copied in, since uv installs the project from it.
var pythonInstalls = map[PythonManager]func(f *Facts, uvCache []Cache) []Step{
	PythonUv:     uvInstall,
	PythonPip:    pipInstall,
	PythonPoetry: poetryInstall,
}

func uvInstall(f *Facts, uvCache []Cache) []Step {
	ws := f.UvWorkspace
	if ws == nil {
		return []Step{
			Copy{Src: []string{"pyproject.toml", "uv.lock"}, Dst: "./"},
			Run{Cmd: "uv sync --frozen --no-dev --no-install-project", Caches: uvCache},
			copySource(),
			Run{Cmd: "uv sync --frozen --no-dev", Caches: uvCache},
		}
	}
	// Dependencies install from the manifests alone; members need their source to build.
	manifests := []string{"pyproject.toml", "uv.lock"}
	for _, dir := range ws.Dirs {
		manifests = append(manifests, dir+"/pyproject.toml")
	}
	sync := "uv sync --frozen --no-dev --package " + shellQuote(f.Python.Name)
	return []Step{
		Copy{Src: manifests, Dst: "./", Parents: true},
		Run{Cmd: sync + " --no-install-workspace", Caches: uvCache},
		copySource(),
		Run{Cmd: sync, Caches: uvCache},
	}
}

// uv venv leaves pip out of the image; uv pip installs the same requirements.
func pipInstall(f *Facts, uvCache []Cache) []Step {
	py := f.Python
	steps := []Step{Run{Cmd: "uv venv " + venv}}
	if len(py.RequirementFiles) > 0 {
		steps = append(steps,
			Copy{Src: py.RequirementFiles, Dst: "./", Parents: true},
			Run{Cmd: "uv pip install -r requirements.txt", Caches: uvCache})
	} else if len(py.Requirements) > 0 {
		steps = append(steps, Run{Cmd: "uv pip install " + shellQuote(py.Requirements...), Caches: uvCache})
	}
	return append(steps, copySource())
}

func poetryInstall(_ *Facts, uvCache []Cache) []Step {
	return []Step{
		Run{Cmd: "uv venv " + venv},
		Env{"UV_TOOL_BIN_DIR", "/usr/local/bin"},
		Run{Cmd: "uv tool install poetry==" + poetryVersion, Caches: uvCache},
		Env{"POETRY_VIRTUALENVS_IN_PROJECT", "true"},
		Env{"POETRY_NO_INTERACTION", "1"},
		Copy{Src: []string{"pyproject.toml", "poetry.lock"}, Dst: "./"},
		Run{Cmd: "poetry install --only main --no-root", Caches: caches("/root/.cache/pypoetry")},
		copySource(),
	}
}

// uvicorn can't read PORT and distroless has no shell, so Python adds --port at startup.
const withPort = "import os, sys; os.execvp(sys.argv[1], sys.argv[1:] + ['--port', os.environ['PORT']])"

// gunicorn binds to 0.0.0.0:$PORT on its own when PORT is set.
// call is the function whose result an entry file assigns, such as app = FastAPI(). Django has
// none and finds its module from manage.py.
var pythonFrameworks = []struct {
	name, dep, server, call string
	cmd                     func(module string) Cmd
}{
	{"Django", "django", "gunicorn", "", func(m string) Cmd { return Cmd{"gunicorn", m} }},
	{"FastAPI", "fastapi", "uvicorn", "FastAPI", func(m string) Cmd { return Cmd{"python", "-c", withPort, "uvicorn", m, "--host", "0.0.0.0"} }},
	{"Flask", "flask", "gunicorn", "Flask", func(m string) Cmd { return Cmd{"gunicorn", m} }},
}

func pythonStart(py *Python) (Cmd, error) {
	var apps []string
	var start Cmd
	for _, fw := range pythonFrameworks {
		if !py.Deps[fw.dep] {
			continue
		}
		module := py.Apps[fw.call]
		if fw.call == "" {
			module = djangoWSGI(py)
		}
		if module == "" {
			continue
		}
		if !py.Deps[fw.server] {
			return nil, fmt.Errorf("this is a %s app but %s isn't in its dependencies; add %s", fw.name, fw.server, fw.server)
		}
		apps = append(apps, fw.name+" ("+module+")")
		start = fw.cmd(module)
	}
	if len(apps) > 1 {
		return nil, fmt.Errorf("this app defines %s; set start in terril.toml to the one to run", strings.Join(apps, " and "))
	}
	if start != nil {
		return start, nil
	}
	if len(py.Entries) > 0 {
		return Cmd{"python", py.Entries[0]}, nil
	}
	return nil, fmt.Errorf("no start command: add a main.py, or a Procfile with a web: line")
}

func djangoWSGI(py *Python) string {
	if py.DjangoProject == "" {
		return ""
	}
	return py.DjangoProject + ".wsgi"
}
