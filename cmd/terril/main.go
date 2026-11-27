package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/LoriKarikari/terril/pack"
)

const usage = `terril turns an app directory into a container image.

  terril plan  [--provider name] [--env KEY]... DIR
      print the Dockerfile
  terril build [--provider name] [--env KEY[=VALUE]]... [-t tag] DIR
      build the image with docker
  terril version
      print the version
  terril help
      print this help

Flags go before DIR. --env makes a variable available to the build steps,
but not to the running app. Without =VALUE it takes the value from the
environment. -t defaults to the directory name.
`

// Release builds set version with -ldflags.
var version string

func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	os.Exit(cli(os.Args[1:]))
}

func cli(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	cmd := args[0]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	case "version", "--version":
		if len(args) > 1 {
			return usageError("version takes no arguments")
		}
		fmt.Println("terril", versionString())
		return 0
	case "plan", "build":
	default:
		return usageError(fmt.Sprintf("unknown command %q", cmd))
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	provider := fs.String("provider", "", "")
	tag := fs.String("t", "", "")
	var env envFlags
	fs.Var(&env, "env", "")
	if err := fs.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		fmt.Print(usage)
		return 0
	} else if err != nil {
		return usageError(err.Error())
	}
	switch {
	case fs.NArg() == 0:
		return usageError("missing DIR")
	case fs.NArg() > 1 && strings.HasPrefix(fs.Arg(1), "-"):
		return usageError("flags go before DIR")
	case fs.NArg() > 1:
		return usageError("one DIR at a time")
	case cmd == "plan" && *tag != "":
		return usageError("-t only applies to build")
	}

	err := run(cmd, fs.Arg(0), *provider, *tag, &env)
	if exit := (*exec.ExitError)(nil); errors.As(err, &exit) && exit.ExitCode() > 0 {
		fmt.Fprintln(os.Stderr, "terril: docker build failed")
		return exit.ExitCode()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "terril:", err)
		return 1
	}
	return 0
}

func usageError(msg string) int {
	fmt.Fprintf(os.Stderr, "terril: %s\n\n%s", msg, usage)
	return 2
}

func run(cmd, dir, provider, tag string, env *envFlags) error {
	plan, err := makePlan(dir, provider, env)
	if err != nil {
		return err
	}
	if cmd == "plan" {
		fmt.Print(plan.Dockerfile())
		return nil
	}
	if err := env.check(); err != nil {
		return err
	}
	if tag == "" {
		if tag, err = defaultTag(dir); err != nil {
			return err
		}
	}
	return build(plan, tag, env)
}

func makePlan(dir, provider string, env *envFlags) (*pack.Plan, error) {
	facts, err := pack.Read(dir)
	if err != nil {
		return nil, err
	}
	// Tests set the date to check end-of-life warnings and refusals without waiting for them.
	if today := os.Getenv("TERRIL_TODAY"); today != "" {
		if facts.Today, err = time.Parse(time.DateOnly, today); err != nil {
			return nil, fmt.Errorf("TERRIL_TODAY: %w", err)
		}
	}
	plan, err := pack.New(facts, provider)
	if err != nil {
		return nil, err
	}
	for _, w := range plan.Warnings {
		fmt.Fprintln(os.Stderr, "terril: warning:", w.Message)
	}
	plan.BuildEnv = env.names
	return plan, nil
}

func build(plan *pack.Plan, tag string, env *envFlags) error {
	fmt.Fprintf(os.Stderr, "terril: building %s as %s\n", plan.Summary(), tag)
	args := []string{"build", "-t", tag, "-f", "-"}
	dockerEnv := os.Environ()
	if len(env.names) > 0 {
		digest, err := env.digest()
		if err != nil {
			return err
		}
		// Provenance attestations record build args, and the digest is derived from secret values.
		args = append(args, "--provenance=false", "--build-arg", pack.BuildEnvArg+"="+digest)
		for _, n := range env.names {
			args = append(args, "--secret", "id="+n+",env="+n)
			dockerEnv = append(dockerEnv, n+"="+env.values[n])
		}
	}
	docker := exec.Command("docker", append(args, plan.Context)...)
	docker.Env = dockerEnv
	docker.Stdin = strings.NewReader(plan.Dockerfile())
	docker.Stdout, docker.Stderr = os.Stdout, os.Stderr
	return docker.Run()
}

var (
	// Docker's rule for a repository name.
	dockerNameRe = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)
	nonAlnumRe   = regexp.MustCompile(`[^a-z0-9]+`)
)

// defaultTag names the image after the directory, turning what docker rejects into dashes, so
// "My App" becomes my-app.
func defaultTag(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	name := strings.ToLower(filepath.Base(abs))
	if !dockerNameRe.MatchString(name) {
		name = strings.Trim(nonAlnumRe.ReplaceAllString(name, "-"), "-")
	}
	if name == "" {
		return "", fmt.Errorf("can't make an image name from %q; pass -t", filepath.Base(abs))
	}
	return name, nil
}
