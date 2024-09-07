# gofuzz

gofuzz runs Golang fuzz tests in parallel.

## Install

```sh
go install github.com/koonix/gofuzz@latest
```

## Usage

Simple example:

```sh
gofuzz -- -fuzztime=10s
```

Elaborate example:

```sh
gofuzz -parallel=5 -run='^FuzzSomeFunc$|^some/pkg/FuzzAnotherFunc$' -- -fuzztime=30s -fuzzminimizetime=2m
```

Usage:

```
Usage of ./gofuzz:
  -gotest string
    	command used for running tests, as whitespace-separated args (default "go test")
  -parallel int
    	max number of parallel tests (default 10)
  -root string
    	root dir of the go project (default ".")
  -run string
    	only run tests where path/to/package/FuzzFuncName matches against this regexp pattern (default ".")
```
