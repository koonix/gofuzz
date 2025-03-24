// Copyright 2024 the gofuzz authors.
// SPDX-License-Identifier: Apache-2.0

package pkgutil

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

type PackageInfo struct {
	Package *packages.Package
	Path    string
}

type SourceFile struct {
	Path    string
	Node    *ast.File
	PkgInfo PackageInfo
}

// Load loads the packages described by the given patterns.
func Load(patterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:  packages.LoadSyntax,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	return pkgs, packageErrors(pkgs)
}

// Render creates [PackageInfo]s and [SourceFile]s from the given packages.
//
// [PackageInfo.Path] is relative to projectRootDir.
//
// [SourceFile.Path] could be relative to projectRootDir or absolute.
func Render(
	pkgs []*packages.Package,
	projectRootDir string,
) (
	pkgInfos []PackageInfo,
	srcFiles []SourceFile,
	err error,
) {
	for _, pkg := range pkgs {
		if pkg.Dir == "" {
			return nil, nil, fmt.Errorf(
				"no directory associated with package %q",
				pkg.ID,
			)
		}
		pkgPath, err := filepath.Rel(projectRootDir, pkg.Dir)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"could not get relative path of package %q at %q: %w",
				pkg.ID, pkg.Dir, err,
			)
		}
		pkgInfo := PackageInfo{
			Package: pkg,
			Path:    pkgPath,
		}
		pkgInfos = append(pkgInfos, pkgInfo)
		for _, fileNode := range pkg.Syntax {
			abs := pkg.Fset.File(fileNode.Pos()).Name()
			filePath, err := filepath.Rel(projectRootDir, abs)
			// The source file could be in a temporary go-build directory,
			// in which case Rel fails and we just use the absolute path.
			if err != nil {
				filePath = abs
			}
			srcFiles = append(srcFiles, SourceFile{
				Path:    filePath,
				Node:    fileNode,
				PkgInfo: pkgInfo,
			})
		}
	}
	return pkgInfos, srcFiles, nil
}

// Funcs runs fn on the function declarations of srcFiles that end in suffix.
func Funcs(
	srcFiles []SourceFile,
	suffix string,
	fn func(file SourceFile, funcDecl *ast.FuncDecl),
) {
	for _, file := range srcFiles {
		if !strings.HasSuffix(file.Path, suffix) {
			continue
		}
		ast.Inspect(file.Node, func(node ast.Node) (descend bool) {
			if _, ok := node.(*ast.File); ok {
				return true
			}
			if funcDecl, ok := node.(*ast.FuncDecl); ok {
				fn(file, funcDecl)
			}
			return false
		})
	}
}

// Seeds calls fn with the path of the
// fuzz seed corpus files of the given packages.
func Seeds(
	ctx context.Context,
	pkgInfos []PackageInfo,
	fn func(seedFilePath string) error,
) error {
	for _, pkg := range pkgInfos {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		default:
		}
		dir := filepath.Join(pkg.Path, "testdata", "fuzz")
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(
			path string,
			entry fs.DirEntry,
			err error,
		) error {
			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			default:
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			return fn(path)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Pattern returns a package pattern that matches the package at the given path.
func Pattern(pkgPath string) (pkgPattern string) {
	if pkgPath == "." {
		return pkgPath
	}
	return "./" + filepath.ToSlash(pkgPath)
}

// packageErrors is similar to [packages.PrintErrors]
// but instead of printing the errors, it accumulates and returns them.
func packageErrors(pkgs []*packages.Package) (pkgErrs error) {
	addErr := func(e error) {
		if pkgErrs == nil {
			pkgErrs = e
		} else {
			pkgErrs = fmt.Errorf("%w\n%w", pkgErrs, e)
		}
	}
	modErr := make(map[*packages.Module]bool)
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, err := range pkg.Errors {
			addErr(err)
		}
		// Print pkg.Module.Error once if present.
		mod := pkg.Module
		if mod != nil && mod.Error != nil && !modErr[mod] {
			modErr[mod] = true
			addErr(errors.New(mod.Error.Err))
		}
	})
	return
}
