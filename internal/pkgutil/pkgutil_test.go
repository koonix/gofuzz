// Copyright 2024 the gofuzz authors.
// SPDX-License-Identifier: Apache-2.0

package pkgutil_test

import (
	"context"
	"go/ast"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/koonix/gofuzz/internal/pkgutil"
	"github.com/stretchr/testify/assert"
)

func Test(t *testing.T) {

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get current test file path")
	}

	srcDir := filepath.Join(filepath.Dir(thisFile), "testdata", "src")
	err := os.Chdir(srcDir)
	if err != nil {
		t.Fatalf("could not chdir to src dir %q: %s", srcDir, err)
	}

	pkgs, err := pkgutil.Load("./...")
	if err != nil {
		t.Fatalf("could not load packages: %s", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("could not get cwd: %s", err)
	}

	pkgInfos, srcFiles, err := pkgutil.Render(pkgs, cwd)
	if err != nil {
		t.Fatalf("could not render packages: %s", err)
	}

	gotSrcFiles := slices.Clone(srcFiles)

	for i := range gotSrcFiles {
		gotSrcFiles[i].Node = nil
		gotSrcFiles[i].PkgInfo.Package = nil
	}

	wantSrcFiles := []pkgutil.SourceFile{
		{
			Path:    filepath.FromSlash("main.go"),
			PkgInfo: pkgutil.PackageInfo{Path: filepath.FromSlash(".")},
		},
		{
			Path:    filepath.FromSlash("pkg1/file1.go"),
			PkgInfo: pkgutil.PackageInfo{Path: filepath.FromSlash("pkg1")},
		},
		{
			Path:    filepath.FromSlash("pkg1/file2.go"),
			PkgInfo: pkgutil.PackageInfo{Path: filepath.FromSlash("pkg1")},
		},
		{
			Path:    filepath.FromSlash("pkg1/subpkg1/subfile1.go"),
			PkgInfo: pkgutil.PackageInfo{Path: filepath.FromSlash("pkg1/subpkg1")},
		},
		{
			Path:    filepath.FromSlash("pkg1/subpkg1/subfile2.go"),
			PkgInfo: pkgutil.PackageInfo{Path: filepath.FromSlash("pkg1/subpkg1")},
		},
		{
			Path:    filepath.FromSlash("pkg2/file3.go"),
			PkgInfo: pkgutil.PackageInfo{Path: filepath.FromSlash("pkg2")},
		},
		{
			Path:    filepath.FromSlash("pkg2/file4.go"),
			PkgInfo: pkgutil.PackageInfo{Path: filepath.FromSlash("pkg2")},
		},
		{
			Path:    filepath.FromSlash("pkg2/subpkg2/subfile3.go"),
			PkgInfo: pkgutil.PackageInfo{Path: filepath.FromSlash("pkg2/subpkg2")},
		},
		{
			Path:    filepath.FromSlash("pkg2/subpkg2/subfile4.go"),
			PkgInfo: pkgutil.PackageInfo{Path: filepath.FromSlash("pkg2/subpkg2")},
		},
	}

	t.Run("Render", func(t *testing.T) {
		assert.Equal(t, wantSrcFiles, gotSrcFiles)
	})

	// ==========

	funcs := make([]string, 0)

	pkgutil.Funcs(srcFiles, "", func(
		file pkgutil.SourceFile,
		funcDecl *ast.FuncDecl,
	) {
		path := filepath.ToSlash(file.Path)
		funcs = append(funcs, path+"/"+funcDecl.Name.Name+"()")
	})

	wantFuncs := []string{
		"main.go/main()",
		"pkg1/file1.go/Func1()",
		"pkg1/file1.go/Func2()",
		"pkg1/file2.go/Func3()",
		"pkg1/file2.go/Func4()",
		"pkg1/subpkg1/subfile1.go/Func1()",
		"pkg1/subpkg1/subfile1.go/Func2()",
		"pkg1/subpkg1/subfile2.go/Func3()",
		"pkg1/subpkg1/subfile2.go/Func4()",
		"pkg2/file3.go/Func1()",
		"pkg2/file3.go/Func2()",
		"pkg2/file4.go/Func3()",
		"pkg2/file4.go/Func4()",
		"pkg2/subpkg2/subfile3.go/Func1()",
		"pkg2/subpkg2/subfile3.go/Func2()",
		"pkg2/subpkg2/subfile4.go/Func3()",
		"pkg2/subpkg2/subfile4.go/Func4()",
	}

	t.Run("Funcs", func(t *testing.T) {
		assert.Equal(t, wantFuncs, funcs)
	})

	// ==========

	funcs = make([]string, 0)

	pkgutil.Funcs(srcFiles, "file3.go", func(
		file pkgutil.SourceFile,
		funcDecl *ast.FuncDecl,
	) {
		path := filepath.ToSlash(file.Path)
		funcs = append(funcs, path+"/"+funcDecl.Name.Name+"()")
	})

	wantFuncs = []string{
		"pkg2/file3.go/Func1()",
		"pkg2/file3.go/Func2()",
		"pkg2/subpkg2/subfile3.go/Func1()",
		"pkg2/subpkg2/subfile3.go/Func2()",
	}

	t.Run("FuncsWithSuffix", func(t *testing.T) {
		assert.Equal(t, wantFuncs, funcs)
	})

	// ==========

	seedPaths := make([]string, 0)

	pkgutil.Seeds(context.Background(), pkgInfos, func(seedFilePath string) error {
		seedPaths = append(seedPaths, seedFilePath)
		return nil
	})

	wantSeedPaths := []string{
		filepath.FromSlash("testdata/fuzz/seed1"),
		filepath.FromSlash("pkg1/testdata/fuzz/seed2"),
		filepath.FromSlash("pkg2/subpkg2/testdata/fuzz/seed3"),
	}

	t.Run("Seeds", func(t *testing.T) {
		assert.Equal(t, wantSeedPaths, seedPaths)
	})
}
