package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/gobwas/glob"
	"gopkg.in/yaml.v3"

	"github.com/rprtr258/punused/internal/lib"
)

const (
	configFilename  = ".punused.yaml"
	_defaultTimeout = 10 * time.Minute
)

type Config struct {
	ExcludedPaths   []glob.Glob
	ExcludedSymbols []string
	Timeout         time.Duration
}

func readYAMLConfig(filename string) (Config, error) {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var config struct {
		Timeout  *string `yaml:"timeout"`
		Excluded struct {
			Paths   []string `yaml:"paths"`
			Symbols []string `yaml:"symbols"`
		} `yaml:"exclude"`
	}
	if err := yaml.Unmarshal(bytes, &config); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	timeout := _defaultTimeout
	if config.Timeout != nil {
		t, err := time.ParseDuration(*config.Timeout)
		if err != nil {
			return Config{}, fmt.Errorf("parse timeout: %w", err)
		}
		timeout = t
	}

	excludedPaths := make([]glob.Glob, len(config.Excluded.Paths))
	for i, path := range config.Excluded.Paths {
		matcher, err := glob.Compile(path)
		if err != nil {
			return Config{}, fmt.Errorf("parse exclude path glob: %w", err)
		}
		excludedPaths[i] = matcher
	}

	return Config{
		ExcludedPaths:   excludedPaths,
		ExcludedSymbols: config.Excluded.Symbols,
		Timeout:         timeout,
	}, nil
}

func run() error {
	// Default to "every go file in the workspace".
	pattern := "**.go"
	if len(os.Args) > 1 {
		pattern = os.Args[1]
	}

	patternGlob, err := glob.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid glob pattern: %w", err)
	}

	config, err := readYAMLConfig(configFilename)
	if err != nil {
		var e syscall.Errno
		if !errors.As(err, &e) || !e.Is(os.ErrNotExist) {
			return fmt.Errorf("read config file: %w", err)
		}

		log.Println("no config file found, using default config")
		config = Config{
			ExcludedPaths:   nil,
			ExcludedSymbols: nil,
			Timeout:         _defaultTimeout,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	workdir, err := os.Getwd()
	if err != nil {
		return err
	}

	return lib.Run(ctx, lib.RunConfig{
		WorkspaceDir:    workdir,
		FilenamePattern: patternGlob,
		ExcludedPaths:   config.ExcludedPaths,
		ExcludedSymbols: config.ExcludedSymbols,
	})
}

func main() {
	log.SetFlags(log.Lshortfile)
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
