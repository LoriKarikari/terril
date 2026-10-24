package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LoriKarikari/wolkpack/pack"
)

const usage = `wolkpack turns an app directory into a container image.

  wolkpack plan  [--provider name] DIR            print the Dockerfile
  wolkpack build [--provider name] [-t tag] DIR   build the image with docker
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	provider := fs.String("provider", "", "force the primary provider (go, python, node)")
	tag := fs.String("t", "", "image tag (build only; default: directory name)")
	fs.Parse(os.Args[2:])
	if fs.NArg() != 1 || (cmd != "plan" && cmd != "build") {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	dir := fs.Arg(0)

	if err := run(cmd, dir, *provider, *tag); err != nil {
		fmt.Fprintln(os.Stderr, "wolkpack:", err)
		os.Exit(1)
	}
}

func run(cmd, dir, provider, tag string) error {
	facts, err := pack.Read(dir)
	if err != nil {
		return err
	}
	plan, err := pack.New(facts, provider)
	if err != nil {
		return err
	}
	dockerfile := plan.Dockerfile()
	if cmd == "plan" {
		fmt.Print(dockerfile)
		return nil
	}

	if tag == "" {
		abs, _ := filepath.Abs(dir)
		tag = strings.ToLower(filepath.Base(abs))
	}
	fmt.Fprintf(os.Stderr, "wolkpack: building %s as %s\n", plan.Summary(), tag)
	docker := exec.Command("docker", "build", "-t", tag, "-f", "-", plan.Context)
	docker.Stdin = strings.NewReader(dockerfile)
	docker.Stdout, docker.Stderr = os.Stdout, os.Stderr
	return docker.Run()
}
