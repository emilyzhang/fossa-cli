// Package php implements analyzers for PHP.
//
// A `BuildTarget` for PHP is the path to the `composer.json`.
package php

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"

	"github.com/fossas/fossa-cli/buildtools/composer"
	"github.com/fossas/fossa-cli/exec"
	"github.com/fossas/fossa-cli/files"
	"github.com/fossas/fossa-cli/log"
	"github.com/fossas/fossa-cli/module"
	"github.com/fossas/fossa-cli/pkg"
)

type Analyzer struct {
	Composer composer.Composer

	Options Options
}

// TODO: strategies:
// - composer show
// - read lockfile
// - read manifest
// - read components

type Options struct {
	Strategy string `mapstructure:"strategy"`
}

func New(opts map[string]interface{}) (*Analyzer, error) {
	log.Logger.Debug("%#v", opts)
	// Set Bower context variables
	composerCmd, _, err := exec.Which("--version", os.Getenv("COMPOSER_BINARY"), "composer")
	if err != nil {
		return nil, errors.Wrap(err, "could not find Composer binary (try setting $COMPOSER_BINARY)")
	}

	// Decode options
	var options Options
	err = mapstructure.Decode(opts, &options)
	if err != nil {
		return nil, err
	}

	analyzer := Analyzer{
		Composer: composer.Composer{
			Cmd: composerCmd,
		},

		Options: options,
	}

	log.Logger.Debugf("analyzer: %#v", analyzer)
	return &analyzer, nil
}

// Discover finds `composer.json`s not a /vendor/ folder
func (a *Analyzer) Discover(dir string) ([]module.Module, error) {
	var modules []module.Module
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Logger.Debugf("failed to access path %s: %s\n", path, err.Error())
			return err
		}

		// Skip the /vendor/ folder
		if info.IsDir() && info.Name() == "vendor" {
			log.Logger.Debugf("skipping `vendor` directory: %s", info.Name())
			return filepath.SkipDir
		}

		if !info.IsDir() && info.Name() == "composer.json" {
			dir := filepath.Dir(path)
			name := filepath.Base(dir)

			// Parse from composer.json and set name if successful
			var composerPackage composer.Manifest
			if err := files.ReadJSON(&composerPackage, path); err == nil {
				name = composerPackage.Name
			}

			log.Logger.Debugf("found Composer package: %s (%s)", path, name)
			modules = append(modules, module.Module{
				Name:        name,
				Type:        pkg.Composer,
				BuildTarget: path,
				Dir:         dir,
			})
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("could not find Composer package manifests: %s", err.Error())
	}

	return modules, nil
}

func (a *Analyzer) Clean(m module.Module) error {
	return a.Composer.Clean(m.Dir)
}

func (a *Analyzer) Build(m module.Module) error {
	return a.Composer.Install(m.Dir)
}

func (a *Analyzer) IsBuilt(m module.Module) (bool, error) {
	_, err := a.Composer.Show(m.Dir)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (a *Analyzer) Analyze(m module.Module) (module.Module, error) {
	imports, graph, err := a.Composer.Dependencies(m.Dir)
	if err != nil {
		return m, err
	}

	for _, i := range imports {
		m.Imports = append(m.Imports, pkg.Import{
			Resolved: pkg.ID{
				Type:     pkg.Composer,
				Name:     i.Name,
				Revision: i.Version,
			},
		})
	}

	g := make(map[pkg.ID]pkg.Package)
	for parent, children := range graph {
		id := pkg.ID{
			Type:     pkg.Composer,
			Name:     parent.Name,
			Revision: parent.Version,
		}
		var deps []pkg.Import
		for _, child := range children {
			deps = append(deps, pkg.Import{
				Resolved: pkg.ID{
					Type:     pkg.Composer,
					Name:     child.Name,
					Revision: child.Version,
				},
			})
		}
		g[id] = pkg.Package{
			ID:      id,
			Imports: deps,
		}
	}
	m.Deps = g

	return m, nil
}
