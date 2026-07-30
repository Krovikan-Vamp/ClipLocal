package discord

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	webhookURL string
	httpClient *http.Client
}

func NewClient(webhookURL string) *Client {
	return &Client{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) PostClip(clipPath, content string) error {
	status, err := c.postMultipart(clipPath, content)
	if err == nil {
		return nil
	}
	if status != http.StatusRequestEntityTooLarge && !strings.Contains(strings.ToLower(err.Error()), "file is too powerful") {
		return err
	}

	fallback := strings.TrimSuffix(clipPath, filepath.Ext(clipPath)) + ".discord.mp4"
	if recodeErr := reencodeSmaller(clipPath, fallback); recodeErr != nil {
		return fmt.Errorf("discord upload failed: %w; fallback transcode failed: %v", err, recodeErr)
	}
	defer os.Remove(fallback)

	_, retryErr := c.postMultipart(fallback, content)
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (c *Client) postMultipart(path string, content string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("content", content); err != nil {
		return 0, err
	}
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return 0, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}

	req, err := http.NewRequest(http.MethodPost, c.webhookURL, &body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("discord webhook returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return resp.StatusCode, nil
}

func reencodeSmaller(inputPath, outputPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", inputPath, "-c:v", "libx264", "-b:v", "1800k", "-c:a", "aac", "-b:a", "96k", "-fs", "24000000", outputPath)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("ffmpeg re-encode failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
