package main

import (
	"fmt"
	"os"
	"strings"
)

type credentialRequest struct {
	File    string
	EnvFile string
	Default string
	Name    string
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
	return credential{}, fmt.Errorf("%s token not provided", req.Name)
}

func (c *cliContext) adminCredential() (credential, error) {
	return loadCredential(credentialRequest{
		File:    c.adminFile,
		EnvFile: "PRISM_ADMIN_TOKEN_FILE",
		Default: defaultAdminTokenFile,
		Name:    "admin",
	})
}
