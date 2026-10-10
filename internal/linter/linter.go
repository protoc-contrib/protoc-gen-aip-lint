// Package linter wraps the Google API Linter (api-linter/v2) for use as
// a protoc plugin. It provides configuration, rule registration, and
// output encoding for lint results in multiple formats.
package linter

import (
	"fmt"
	"sync"

	"github.com/googleapis/api-linter/v2/lint"
	"github.com/googleapis/api-linter/v2/rules"
)

var (
	registry     = lint.NewRuleRegistry()
	registryOnce sync.Once
	registryErr  error
)

func initRegistry() error {
	registryOnce.Do(func() {
		if registryErr = rules.Add(registry); registryErr != nil {
			return
		}
		registryErr = registerCustomRules(registry)
	})
	return registryErr
}

// Config controls which rules the linter applies and how it resolves them.
//
// When Path is set, rules are loaded from the given YAML configuration file
// (see [lint.ReadConfigsFromFile] for the expected format). Setting
// IgnoreCommentDisables to true causes the linter to disregard inline
// disable comments, enforcing every enabled rule unconditionally.
type Config struct {
	// Path is the filesystem path to a YAML linter configuration file.
	// An empty string means no file-based configuration is loaded.
	Path string

	// IgnoreCommentDisables, when true, makes the linter ignore inline
	// "// (-- api-linter: ... --)" disable directives in proto sources.
	IgnoreCommentDisables bool
}

// Response is the lint result for a single proto file. It is an alias for
// [lint.Response] so callers do not need to import the api-linter package
// directly.
type Response = lint.Response

// New creates a configured [lint.Linter] backed by the full set of built-in
// AIP rules, plus this plugin's own (see [UpdateMaskWritableFields]). It returns an error if rule registration fails or the
// configuration file at [Config.Path] cannot be read.
func New(config *Config) (*lint.Linter, error) {
	if err := initRegistry(); err != nil {
		return nil, fmt.Errorf("registering lint rules: %w", err)
	}

	var options lint.Configs

	if config.Path != "" {
		fconfig, err := lint.ReadConfigsFromFile(config.Path)
		if err != nil {
			return nil, err
		}

		options = append(options, fconfig...)
	}

	linter := lint.New(registry, options, lint.IgnoreCommentDisables(config.IgnoreCommentDisables))
	return linter, nil
}
