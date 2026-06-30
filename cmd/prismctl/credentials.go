package main

import (
	"fmt"
	"os"
	"strings"
)

type credentialRequest struct {
	File       string
	Raw        string
	EnvFile    string
	EnvRaw     string
	Default    string
	Name       string
	AllowEmpty bool
}

type credential struct {
	Secret   string
	Source   string
	Warnings []string
}

func loadCredential(req credentialRequest) (credential, error) {
	if req.File != "" {
		secret, err := readAll(req.File)
		if err != nil {
			return credential{}, fmt.Errorf("read %s token file %q: %w", req.Name, req.File, err)
		}
		return credential{Secret: secret, Source: "file:" + req.File}, nil
	}
	if file := strings.TrimSpace(os.Getenv(req.EnvFile)); file != "" {
		secret, err := readAll(file)
		if err != nil {
			return credential{}, fmt.Errorf("read %s token file %q: %w", req.Name, file, err)
		}
		return credential{Secret: secret, Source: "env-file:" + req.EnvFile}, nil
	}
	if req.Default != "" {
		if secret, err := readAll(req.Default); err == nil {
			return credential{Secret: secret, Source: "file:" + req.Default}, nil
		}
	}
	if req.Raw != "" {
		warning := fmt.Sprintf("reading %s token from raw flag is less safe than token file", req.Name)
		warnf(warning)
		return credential{Secret: strings.TrimSpace(req.Raw), Source: "raw-flag", Warnings: []string{warning}}, nil
	}
	if raw := strings.TrimSpace(os.Getenv(req.EnvRaw)); raw != "" {
		warning := fmt.Sprintf("reading %s token from %s is less safe than token file", req.Name, req.EnvRaw)
		warnf(warning)
		return credential{Secret: raw, Source: "env:" + req.EnvRaw, Warnings: []string{warning}}, nil
	}
	if req.AllowEmpty {
		return credential{}, nil
	}
	return credential{}, fmt.Errorf("%s token not provided", req.Name)
}

func (c *cliContext) rootCredential() (credential, error) {
	return loadCredential(credentialRequest{
		File:    c.rootFile,
		Raw:     c.rootToken,
		EnvFile: "PRISM_ROOT_TOKEN_FILE",
		EnvRaw:  "PRISM_ROOT_TOKEN",
		Default: defaultRootTokenFile,
		Name:    "root",
	})
}

func (c *cliContext) adminCredential() (credential, error) {
	return loadCredential(credentialRequest{
		File:    c.adminFile,
		Raw:     c.adminToken,
		EnvFile: "PRISM_ADMIN_TOKEN_FILE",
		EnvRaw:  "PRISM_ADMIN_TOKEN",
		Default: defaultAdminTokenFile,
		Name:    "admin",
	})
}
