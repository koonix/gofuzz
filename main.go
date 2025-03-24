// Copyright 2024 the gofuzz authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/koonix/gofuzz/internal/pkgutil"
)

type fuzzJob struct {
	fuzzFuncName string
	file         pkgutil.SourceFile
	cmd          string
	output       []byte
	err          error
}

func main() {

	conf := handleArgs()

	if conf.chdir != "." {
		err := os.Chdir(conf.chdir)
		if err != nil {
			die(fmt.Errorf("could not change directory to %q: %w", conf.chdir, err))
		}
	}

	pkgs, err := pkgutil.Load(conf.packagePatterns...)
	if err != nil {
		die(fmt.Errorf("could not load packages %v: %w", conf.packagePatterns, err))
	}

	cwd, err := os.Getwd()
	if err != nil {
		die(fmt.Errorf("could not get working directory: %w", err))
	}

	pkgInfos, srcFiles, err := pkgutil.Render(pkgs, cwd)
	if err != nil {
		die(fmt.Errorf("could not get package files: %w", err))
	}

	jobs, err := createFuzzJobs(srcFiles, conf.runRegexp)
	if err != nil {
		die(fmt.Errorf("could not create jobs: %w", err))
	}

	doneJobs := make(chan fuzzJob, 1)
	wg := new(sync.WaitGroup)
	sem := make(chan struct{}, conf.parallel)
	ctx := notifyContext()

	go func() {
		defer func() {
			wg.Wait()
			close(doneJobs)
		}()
		for _, job := range jobs {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			wg.Add(1)
			go func() {
				defer func() {
					<-sem
					wg.Done()
				}()
				cmd := createFuzzCmd(
					ctx,
					conf.gotestArgs,
					conf.gotestExtraArgs,
					pkgutil.Pattern(job.file.PkgInfo.Path),
					job.fuzzFuncName,
				)
				job.cmd = cmd.String()
				job.output, job.err = cmd.CombinedOutput()
				doneJobs <- job
			}()
		}
	}()

	failedJobs := 0
	init := false

	for job := range doneJobs {
		if !init {
			fmt.Print("\n")
			init = true
		}
		fmt.Printf("========== %s - %s ==========\n\n", job.fuzzFuncName, job.file.Path)
		fmt.Printf("cmd:\n%s\n\n", job.cmd)
		if len(job.output) > 0 {
			for range 2 {
				job.output = bytes.TrimSuffix(job.output, []byte{'\n'})
			}
			fmt.Printf("output:\n%s\n\n", job.output)
		}
		if job.err != nil && !strings.Contains(job.err.Error(), "exit status") {
			fmt.Printf("error:\n%s\n\n", job.err)
		}
		if job.err != nil {
			failedJobs++
		}
	}

	err = pkgutil.SeedCorpusFiles(ctx, pkgInfos, func(path string) error {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("could not open file %q: %w", path, err)
		}
		defer file.Close()
		fmt.Printf("========== Seed %s ==========\n\n", path)
		_, err = io.Copy(os.Stdout, file)
		if err != nil {
			return fmt.Errorf("could not copy %q to stdout: %w", path, err)
		}
		fmt.Printf("\n")
		return nil
	})
	if err != nil {
		fmt.Printf("==========\n\n")
		die(err)
	}

	if failedJobs > 0 {
		fmt.Printf("==========\n\n")
		die("FAIL")
	}
}

func handleArgs() (conf struct {
	chdir           string
	parallel        int
	runRegexp       *regexp.Regexp
	packagePatterns []string
	gotestArgs      []string
	gotestExtraArgs []string
}) {

	const helpText = `Usage: gofuzz [OPTIONS...] [PACKAGES...] [-- GOTESTARGS...]

gofuzz runs multiple Go fuzz tests.

PACKAGES are package patterns, as accepted by the go test command.
GOTESTARGS are extra args passed to the go test command.

Options:
`

	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), helpText)
		flag.PrintDefaults()
	}

	flag.StringVar(&conf.chdir, "C", ".", "run as if the program was started in this path")
	flag.IntVar(&conf.parallel, "parallel", 1, "maximum number of fuzz tests to run simultaneously")
	run := flag.String("run", ".", "run only those fuzz tests matching the regular expression")
	gotest := flag.String("gotest", "go test", "command used for running tests, as whitespace-separated args")

	conf.gotestExtraArgs = make([]string, 0)
	i := slices.Index(os.Args, "--")
	if i != -1 {
		conf.gotestExtraArgs = os.Args[i+1:]
		os.Args = os.Args[:i]
	}

	flag.Parse()

	conf.runRegexp = regexp.MustCompile(*run)
	conf.gotestArgs = strings.Fields(*gotest)

	conf.packagePatterns = flag.Args()
	if len(conf.packagePatterns) == 0 {
		conf.packagePatterns = []string{"."}
	}

	return
}

func createFuzzJobs(
	srcFiles []pkgutil.SourceFile,
	re *regexp.Regexp,
) (
	jobs []fuzzJob,
	err error,
) {
	for _, file := range srcFiles {
		if !strings.HasSuffix(file.Path, "_test.go") {
			continue
		}
		ast.Inspect(file.Node, func(node ast.Node) (descend bool) {
			if _, ok := node.(*ast.File); ok {
				return true
			}
			fn, ok := node.(*ast.FuncDecl)
			if !ok {
				return false
			}
			fuzzFuncName := fn.Name.Name
			if !strings.HasPrefix(fuzzFuncName, "Fuzz") {
				return false
			}
			if re.String() != "." && !re.MatchString(fuzzFuncName) {
				return false
			}
			jobs = append(jobs, fuzzJob{
				file:         file,
				fuzzFuncName: fuzzFuncName,
			})
			return false
		})
	}
	return jobs, nil
}

func createFuzzCmd(
	ctx context.Context,
	gotestArgs, extraArgs []string,
	pkgPattern string,
	fuzzFuncName string,
) *exec.Cmd {

	args := make([]string, 0, len(gotestArgs)+len(extraArgs)+3)

	args = append(args, gotestArgs...)
	args = append(args, extraArgs...)
	re := fmt.Sprintf("^%s$", regexp.QuoteMeta(fuzzFuncName))
	args = append(args, "-run="+re, "-fuzz="+re, pkgPattern)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.WaitDelay = 10 * time.Second
	cmd.Cancel = func() error {
		err := cmd.Process.Signal(os.Interrupt)
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			err = cmd.Process.Kill()
		}
		return err
	}

	return cmd
}

// notifyContext is similar to [signal.NotifyContext]
// but it closes the context with a cause.
func notifyContext() context.Context {
	ctx, cancel := context.WithCancelCause(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch,
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGHUP,
	)
	go func() {
		for sig := range ch {
			cancel(errors.New("signal: " + sig.String()))
			signal.Stop(ch)
			return
		}
	}()
	return ctx
}

func die(v any) {
	fmt.Println(v)
	os.Exit(1)
}
