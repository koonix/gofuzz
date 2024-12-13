# gofuzz

gofuzz runs Golang fuzz tests in parallel.

## Install

```sh
go install github.com/koonix/gofuzz@latest
```

## Usage

To run all fuzz tests in the project:

```sh
gofuzz
```

To pass arguments to `go test`, put them after a `--`:


```sh
gofuzz -- -fuzztime=10s
```

To configure which fuzz tests to run and the number of tests running in parallel:

```sh
gofuzz -match='^dir1/dir2/FuzzFunc1$|/FuzzFunc2$' -parallel=5
```

Use in GitHub Actions:

```yaml
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5

      - name: Run tests
        run: go test -v ./...

      - name: Run fuzz tests
        run: go run github.com/koonix/gofuzz@latest -- -fuzztime=30s
```

Full usage:

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
    	only operate on functions where this regexp matches against "path/to/package/FuzzFuncName" (default ".")
  -parallel int
    	max number of parallel tests (default 10)
  -root string
    	root dir of the go project (default ".")
```
