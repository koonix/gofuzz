# gofuzz

gofuzz runs Golang fuzz tests in parallel.

## Install

```sh
go install github.com/koonix/gofuzz@latest
```

## Usage

Example 1:

```sh
gofuzz -- -fuzztime=10s
```

Example 2:

```sh
gofuzz -parallel=5 -match='/FuzzFunc1$|^some/pkg/FuzzFunc2$' -- -fuzztime=30s -fuzzminimizetime=2m
```

Usage:

```
Usage: gofuzz [OPTIONS...] [-- GOTESTARGS...]

gofuzz runs Golang fuzz tests in parallel.
GOTESTARGS are extra args passed to the go test command.

Options:
  -gotest string
    	command used for running tests, as whitespace-separated args (default "go test")
  -list
    	list fuzz function paths and exit
  -match string
    	only operate on functions where this regexp matches against path/to/package/FuzzFuncName (default ".")
  -parallel int
    	max number of parallel tests (default 10)
  -root string
    	root dir of the go project (default ".")
```
