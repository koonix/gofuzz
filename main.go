package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// maxForks is the maximum number of simultaneous `go test` commands to run
const maxForks = 10

// fuzz contains the name of a fuzz function and the package path it resides in
type fuzz struct {
	fn  string
	pkg string
}

// result contains a fuzzing result
type result struct {
	fuzz
	err    error
	output string
}

// fuzzRgx is a regexp that matches go fuzz functions
var fuzzRgx *regexp.Regexp

func init() {
	fuzzRgx = regexp.MustCompile(`^func\s+(Fuzz\w+)`)
}

func main() {

	// success indicates what the exit status of gofuzz should be
	var success atomic.Bool
	success.Store(true)

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
		syscall.SIGABRT,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
	)
	go func() {
		for sig := range sigChan {
			cancel(errors.New("received signal " + sig.String()))
		}
	}()

	// fuzzChan contains fuzz functions to run
	fuzzChan := make(chan fuzz, 1024)

	// find fuzz functions in go test files and send them to fuzzChan
	go func() {
		defer close(fuzzChan)
		err := filepath.WalkDir(".", func(
			path string,
			entry fs.DirEntry,
			err error,
		) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf(`could not open file "%s": %w`, path, err)
			}
			defer file.Close()
			sc := bufio.NewScanner(file)
			for sc.Scan() {
				matches := fuzzRgx.FindStringSubmatch(sc.Text())
				if matches == nil || len(matches) < 2 {
					continue
				}
				fuzzChan <- fuzz{
					pkg: filepath.Dir(path),
					fn:  matches[1],
				}
			}
			err = sc.Err()
			if err != nil {
				return fmt.Errorf(`could not scan "%s": %w`, path, err)
			}
			return nil
		})
		if err != nil {
			cancel(fmt.Errorf("could not walk dir: %w", err))
			success.Store(false)
		}
	}()

	// resultChan contains fuzzing results
	resultChan := make(chan result, 1024)

	// spawnChan is filled with data
	// to however many go commands we want to run in parallel.
	// we consume one datum from it before we spawn a command,
	// and we write one datum to it after a spawned command is finished.
	spawnChan := make(chan struct{}, 1024)

	// fill spawnChan.
	go func() {
		for i := 0; i < maxForks; i++ {
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
			cmd := exec.CommandContext(
				ctx,
				"go",
				"test",
				fmt.Sprintf("-run=^%s$", fuzz.fn),
				fmt.Sprintf("-fuzz=^%s$", fuzz.fn),
				"-fuzztime=30s",
				"./"+fuzz.pkg,
			)
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
		fmt.Printf("===== %s - %s() =====\n", r.pkg, r.fn)
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
	err := filepath.WalkDir(".", func(
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

	// exit with the appropriate status
	if success.Load() == true {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}
