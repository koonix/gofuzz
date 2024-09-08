package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const helpText = `Usage: gofuzz [OPTIONS...] [-- GOTESTARGS...]

gofuzz runs Golang fuzz tests in parallel.
GOTESTARGS are extra args passed to the go test command.

Options:
`

// fuzz contains the name of a fuzz function and the package path it resides in
type fuzz struct {
	fn       string
	pkg      string
	fullpath string
}

// result contains a fuzzing result
type result struct {
	fuzz
	err    error
	output string
}

func main() {

	// handle cli flags
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, helpText)
		flag.PrintDefaults()
	}
	maxParallel := flag.Int("parallel", 10, "max number of parallel tests")
	runPtrn := flag.String("run", ".", "only run tests where path/to/package/FuzzFuncName matches against this regexp pattern")
	root := flag.String("root", ".", "root dir of the go project")
	goTest := flag.String("gotest", "go test", "command used for running tests, as whitespace-separated args")
	list := flag.Bool("list", false, "list fuzz function paths and exit")
	flag.Parse()
	runRgx := regexp.MustCompile(*runPtrn)
	goTestFields := strings.Fields(*goTest)

	// chdir to root
	err := os.Chdir(*root)
	if err != nil {
		panic(fmt.Errorf(`could not change directory to "%s": %w`, *root, err))
	}

	// context allows canceling the running commands
	ctx, cancel := context.WithCancelCause(context.Background())

	// cancel the context upon receiving signals that typically terminate programs
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGHUP,
		syscall.SIGPIPE,
		syscall.SIGQUIT,
	)
	go func() {
		for sig := range sigChan {
			cancel(errors.New("received signal " + sig.String()))
		}
	}()

	// success indicates what the exit status of gofuzz should be
	var success atomic.Bool
	success.Store(true)

	// exit with the appropriate status
	defer func() {
		if success.Load() {
			os.Exit(0)
		} else {
			os.Exit(1)
		}
	}()

	// fuzzRgx is a regexp that matches go fuzz functions
	fuzzRgx := regexp.MustCompile(`^func\s+(Fuzz\w+)`)

	// fuzzChan contains fuzz functions to run
	fuzzChan := make(chan fuzz, 1024)

	// find fuzz functions in go test files and send them to fuzzChan
	go func() {
		defer close(fuzzChan)
		err := filepath.WalkDir(".", func(
			p string,
			entry fs.DirEntry,
			err error,
		) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(p, "_test.go") {
				return nil
			}
			file, err := os.Open(p)
			if err != nil {
				return fmt.Errorf(`could not open file "%s": %w`, p, err)
			}
			defer file.Close()
			sc := bufio.NewScanner(file)
			for sc.Scan() {
				matches := fuzzRgx.FindStringSubmatch(sc.Text())
				if matches == nil || len(matches) < 2 {
					continue
				}
				fn := matches[1]
				pkg := path.Clean(path.Dir(filepath.ToSlash(p)))
				fullpath := pkg + "/" + fn
				if runRgx.MatchString(fullpath) {
					fuzzChan <- fuzz{
						fn:       fn,
						pkg:      pkg,
						fullpath: fullpath,
					}
				}
			}
			err = sc.Err()
			if err != nil {
				return fmt.Errorf(`could not scan "%s": %w`, p, err)
			}
			return nil
		})
		if err != nil {
			cancel(fmt.Errorf("could not walk dir: %w", err))
			success.Store(false)
		}
	}()

	// if the list option is set, list fuzz function paths and exit
	if *list {
		for fuzz := range fuzzChan {
			fmt.Println(fuzz.fullpath)
		}
		return
	}

	// resultChan contains fuzzing results
	resultChan := make(chan result, 1024)

	// spawnChan is filled with data
	// to however many go commands we want to run in parallel.
	// we consume one datum from it before we spawn a command,
	// and we write one datum to it after a spawned command is finished.
	spawnChan := make(chan struct{}, 1024)

	// fill spawnChan.
	go func() {
		for i := 0; i < *maxParallel; i++ {
			spawnChan <- struct{}{}
		}
	}()

	// get fuzz functions from fuzzChan and run them using `go test`
	go func() {
		var wg sync.WaitGroup
		defer func() {
			wg.Wait()
			close(resultChan)
			close(spawnChan)
		}()
		for fuzz := range fuzzChan {
			<-spawnChan
			args := make([]string, len(goTestFields))
			copy(args, goTestFields)
			args = append(args,
				"./"+fuzz.pkg,
				fmt.Sprintf("-run=^%s$", fuzz.fn),
				fmt.Sprintf("-fuzz=^%s$", fuzz.fn),
			)
			args = append(args, flag.Args()...)
			cmd := exec.CommandContext(ctx, args[0], args[1:]...)
			cmd.WaitDelay = 10 * time.Second
			cmd.Cancel = func() error {
				return cmd.Process.Signal(syscall.SIGTERM)
			}
			wg.Add(1)
			go func() {
				defer func() {
					spawnChan <- struct{}{}
					wg.Done()
				}()
				output, err := cmd.CombinedOutput()
				resultChan <- result{
					fuzz:   fuzz,
					output: string(output),
					err:    err,
				}
			}()
		}
	}()

	// print fuzzing results
	for r := range resultChan {
		fmt.Printf("===== %s/%s =====\n", r.pkg, r.fn)
		fmt.Println(r.output)
		if r.err != nil {
			success.Store(false)
			if !strings.Contains(r.err.Error(), "exit status") {
				fmt.Println(r.err)
				fmt.Println()
			}
		}
	}

	// print the contents of seed corpus entry files
	err = filepath.WalkDir(".", func(
		path string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.Contains(filepath.ToSlash(path), "/testdata/fuzz/") {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf(`could not open file "%s": %w`, path, err)
		}
		defer file.Close()
		fmt.Printf("===== %s =====\n", path)
		_, err = io.Copy(os.Stdout, file)
		if err != nil {
			return fmt.Errorf(`io.Copy of "%s" failed: %w`, path, err)
		}
		fmt.Println()
		return nil
	})
	if err != nil {
		panic(fmt.Errorf("could not walk dir: %w", err))
	}
}
