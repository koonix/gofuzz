# gofuzz

Unlike regular tests,
only a single fuzz test can be executed using the Go command.

gofuzz allows running multiple or all fuzz tests in a project
back to back or in parallel.

## Install

```sh
go install github.com/koonix/gofuzz@latest
```

## Usage

Usage is similar to running regular tests.

To run fuzz tests of pkg1 and pkg2:

```sh
gofuzz ./pkg1 ./dir/pkg2
```

To run all fuzz tests in the project:

```sh
gofuzz ./...
```

To pass arguments to `go test`, put them after a `--`:


```sh
gofuzz ./... -- -fuzztime=10s
```

To configure which fuzz tests to run and the number of tests running in parallel:

```sh
gofuzz -run='FuzzFunc1|FuzzFunc2' -parallel=5 ./... -- -fuzztime=10s
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
        run: go run github.com/koonix/gofuzz@latest ./... -- -fuzztime=30s
```

Full usage:

```
Usage: gofuzz [OPTIONS...] [PACKAGES...] [-- GOTESTARGS...]

gofuzz runs multiple Go fuzz tests.

PACKAGES are package patterns, as accepted by the go test command.
GOTESTARGS are extra args passed to the go test command.

Options:
  -C string
    	run as if the program was started in this path (default ".")
  -gotest string
    	command used for running tests, as whitespace-separated args (default "go test")
  -parallel int
    	maximum number of fuzz tests to run simultaneously (default 1)
  -run string
    	run only those fuzz tests matching the regular expression (default ".")
```
