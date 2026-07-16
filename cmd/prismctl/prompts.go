package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type promptUploadResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int32  `json:"version"`
	Hash    string `json:"hash"`
}

func adminPromptsUploadCommand(ctx *cliContext) *cobra.Command {
	var name, file, expectedHash string
	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload a prompt version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" || file == "" {
				return fmt.Errorf("--name and --file are required")
			}
			api, warnings, err := ctx.sourceAPI()
			if err != nil {
				return err
			}
			out, err := api.uploadPrompt(cmd.Context(), name, file, expectedHash)
			if err != nil {
				return renderError(ctx, "admin", "prompts_upload", err, warnings)
			}
			return render(ctx, "admin", "prompts_upload", out, warnings)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Prompt name, e.g. worker/planner/analysis/extractor")
	cmd.Flags().StringVar(&file, "file", "", "Prompt file path")
	cmd.Flags().StringVar(&expectedHash, "hash", "", "Expected SHA-256 hash")
	return cmd
}

func (a *sourceAPI) uploadPrompt(ctx context.Context, name, path, expectedHash string) (promptUploadResponse, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return promptUploadResponse{}, err
	}
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	if err := writer.WriteField("name", name); err != nil {
		return promptUploadResponse{}, err
	}
	if expectedHash != "" {
		if err := writer.WriteField("hash", expectedHash); err != nil {
			return promptUploadResponse{}, err
		}
	}
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return promptUploadResponse{}, err
	}
	if _, err := part.Write(body); err != nil {
		return promptUploadResponse{}, err
	}
	if err := writer.Close(); err != nil {
		return promptUploadResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/admin/prompts", &form)
	if err != nil {
		return promptUploadResponse{}, err
	}
	req.Header.Set("X-PRISM-TOKEN", a.token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := a.client.Do(req)
	if err != nil {
		return promptUploadResponse{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return promptUploadResponse{}, fmt.Errorf("API POST /admin/prompts: %s", res.Status)
	}
	var out promptUploadResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return promptUploadResponse{}, err
	}
	return out, nil
}
