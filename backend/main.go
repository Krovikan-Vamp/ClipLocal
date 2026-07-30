package main

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"cliplocal/backend/config"
	"cliplocal/backend/discord"
	"cliplocal/backend/hooks"
	"cliplocal/backend/obsconfig"
	"cliplocal/backend/obsws"
	"cliplocal/backend/pipeline"
	"cliplocal/backend/store"
	"cliplocal/backend/tray"
)

type appState struct {
	cfgPath       string
	cfgMu         sync.RWMutex
	cfg           *config.Config
	store         *store.Store
	obs           *obsws.Client
	pipeline      *pipeline.Pipeline
	apmTracker    *hooks.APMTracker
	ffmpegPath    string
	titleMu       sync.RWMutex
	lastTitle     string
	lastAPM       int
	discordClient *discord.Client
}

type trimRequest struct {
	In         float64 `json:"in"`
	Out        float64 `json:"out"`
	InSeconds  float64 `json:"in_seconds"`
	OutSeconds float64 `json:"out_seconds"`
	Start      float64 `json:"start_seconds"`
	End        float64 `json:"end_seconds"`
}

func main() {
	if runtime.GOOS == "windows" {
		runtime.LockOSThread()
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	cfgPath := filepath.Join(wd, "config.yaml")
	cfg, err := ensureConfig(cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if !filepath.IsAbs(cfg.OutputDir) {
		cfg.OutputDir = filepath.Join(wd, cfg.OutputDir)
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		log.Fatal(err)
	}

	db, err := store.Open(filepath.Join(wd, "clips.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	tray.Start()

	_ = obsconfig.GenerateOBSConfig(filepath.Join(wd, "obs-portable-config"), cfg)

	state := &appState{
		cfgPath:       cfgPath,
		cfg:           cfg,
		store:         db,
		apmTracker:    hooks.StartAPMTracker(),
		ffmpegPath:    "ffmpeg",
		discordClient: discord.NewClient(cfg.DiscordWebhookURL),
	}
	defer state.apmTracker.Stop()

	if cfg.OBS.Path != "" {
		if err := launchOBS(cfg); err != nil {
			log.Printf("failed to launch OBS: %v", err)
		}
	}

	obsURL := fmt.Sprintf("ws://127.0.0.1:%d", cfg.OBS.Port)
	state.obs = obsws.NewClient(obsURL, cfg.OBS.Password)
	if err := waitForOBS(state.obs, 30*time.Second); err != nil {
		log.Printf("OBS websocket unavailable: %v", err)
	} else if err := state.obs.StartReplayBuffer(); err != nil {
		log.Printf("failed to start replay buffer: %v", err)
	}
	defer state.obs.Disconnect()

	if _, err := hooks.RegisterLowLevelKeyboardHook(func() {
		title, _ := hooks.GetForegroundWindowTitle()
		state.setCaptureMeta(title, state.apmTracker.CurrentAPM())
		if state.obs != nil {
			if err := state.obs.SaveReplayBuffer(); err != nil {
				log.Printf("failed to save replay buffer: %v", err)
			}
		}
	}); err != nil {
		log.Printf("failed to register hotkey hook: %v", err)
	}

	state.pipeline = pipeline.NewPipeline(cfg.OutputDir, cfg.OutputDir, state.ffmpegPath, db, state.currentCaptureAPM, state.currentCaptureTitle)
	if err := state.pipeline.Start(); err != nil {
		log.Printf("failed to start pipeline: %v", err)
	} else {
		defer state.pipeline.Stop()
	}

	go state.sampleAPM()
	go state.pruneStorage()

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: withCORS(state.routes()),
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server error: %v", err)
		}
	}()
	defer server.Shutdown(context.Background())

	if runtime.GOOS == "windows" {
		hooks.StartMessageLoop()
		return
	}
	select {}
}

func ensureConfig(path string) (*config.Config, error) {
	cfg, err := config.LoadConfig(path)
	if err == nil {
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	cfg = &config.Config{}
	if err := config.SaveConfig(path, cfg); err != nil {
		return nil, err
	}
	return config.LoadConfig(path)
}

func launchOBS(cfg *config.Config) error {
	args := []string{
		"--websocket_port", strconv.Itoa(cfg.OBS.Port),
		"--websocket_password", cfg.OBS.Password,
		"--minimize-to-tray",
		"--startreplaybuffer",
	}
	cmd := exec.Command(cfg.OBS.Path, args...)
	return cmd.Start()
}

func waitForOBS(client *obsws.Client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := client.Connect(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(time.Second)
	}
	return lastErr
}

func (a *appState) sampleAPM() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if a.store != nil && a.apmTracker != nil {
			_ = a.store.InsertAPMSample(a.apmTracker.CurrentAPM())
		}
	}
}

func (a *appState) pruneStorage() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		a.cfgMu.RLock()
		maxGB := a.cfg.MaxStorageGB
		a.cfgMu.RUnlock()
		if maxGB <= 0 {
			continue
		}
		limitBytes := int64(maxGB * 1024 * 1024 * 1024)
		for {
			total, err := a.store.TotalClipSizeBytes()
			if err != nil || total <= limitBytes {
				break
			}
			fp, thumb, err := a.store.DeleteOldestUnsharedClip()
			if err != nil {
				log.Printf("storage pruning: %v", err)
				break
			}
			_ = os.Remove(fp)
			_ = os.Remove(thumb)
			log.Printf("pruned clip: %s (storage was %d bytes, limit %d)", fp, total, limitBytes)
		}
	}
}

func (a *appState) setCaptureMeta(title string, apm int) {
	a.titleMu.Lock()
	defer a.titleMu.Unlock()
	a.lastTitle = title
	a.lastAPM = apm
}

func (a *appState) currentCaptureTitle() string {
	a.titleMu.RLock()
	defer a.titleMu.RUnlock()
	return a.lastTitle
}

func (a *appState) currentCaptureAPM() int {
	a.titleMu.RLock()
	defer a.titleMu.RUnlock()
	return a.lastAPM
}

func (a *appState) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/clips", a.handleClips)
	mux.HandleFunc("/clips/", a.handleClipAction)
	mux.HandleFunc("/apm/stream", a.handleAPMStream)
	mux.HandleFunc("/config", a.handleConfig)
	mux.HandleFunc("/thumbs/", a.handleThumbnail)
	return mux
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if isLocalOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalOrigin(origin string) bool {
	return strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "https://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") || strings.HasPrefix(origin, "https://127.0.0.1") || strings.Contains(origin, "localhost")
}

func (a *appState) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *appState) handleClips(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clips, err := a.store.ListClips()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, clips)
}

func (a *appState) handleClipAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/clips/"), "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]
	switch action {
	case "trim":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleTrim(w, r, id)
	case "share":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleShare(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (a *appState) handleTrim(w http.ResponseWriter, r *http.Request, id string) {
	clip, err := a.store.GetClip(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var req trimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	in := firstPositive(req.In, req.InSeconds, req.Start)
	out := firstPositive(req.Out, req.OutSeconds, req.End)
	if out <= in {
		http.Error(w, "out point must be greater than in point", http.StatusBadRequest)
		return
	}

	newID := randomID()
	trimmedPath := strings.TrimSuffix(clip.Filepath, filepath.Ext(clip.Filepath)) + "_trim.mp4"
	thumbPath := strings.TrimSuffix(clip.ThumbnailPath, filepath.Ext(clip.ThumbnailPath)) + "_trim.jpg"

	if err := runFFmpeg("ffmpeg", "-y", "-ss", fmt.Sprintf("%.3f", in), "-to", fmt.Sprintf("%.3f", out), "-i", clip.Filepath, "-c:v", "libx264", "-c:a", "aac", trimmedPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := runFFmpeg("ffmpeg", "-y", "-i", trimmedPath, "-vf", "thumbnail,scale=640:-1", "-frames:v", "1", thumbPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	info, err := os.Stat(trimmedPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	trimmed := store.Clip{
		ID:              newID,
		Filepath:        trimmedPath,
		ThumbnailPath:   thumbPath,
		CreatedAt:       time.Now().UTC(),
		GameTitle:       clip.GameTitle,
		APMAtCapture:    clip.APMAtCapture,
		DurationSeconds: out - in,
		SizeBytes:       info.Size(),
	}
	if err := a.store.InsertClip(trimmed); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, trimmed)
}

func (a *appState) handleShare(w http.ResponseWriter, r *http.Request, id string) {
	clip, err := a.store.GetClip(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	a.cfgMu.RLock()
	webhook := a.cfg.DiscordWebhookURL
	a.cfgMu.RUnlock()
	if webhook == "" {
		http.Error(w, "discord_webhook_url is not configured", http.StatusBadRequest)
		return
	}

	a.discordClient = discord.NewClient(webhook)
	if err := a.discordClient.PostClip(clip.Filepath, clip.GameTitle); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := a.store.MarkShared(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shared": true})
}

func (a *appState) handleAPMStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			payload, _ := json.Marshal(map[string]int{"apm": a.apmTracker.CurrentAPM()})
			_, _ = fmt.Fprintf(w, "event: apm\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (a *appState) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.cfgMu.RLock()
		defer a.cfgMu.RUnlock()
		writeJSON(w, http.StatusOK, a.cfg)
	case http.MethodPut:
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := config.SaveConfig(a.cfgPath, &cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		saved, err := config.LoadConfig(a.cfgPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !filepath.IsAbs(saved.OutputDir) {
			saved.OutputDir = filepath.Join(filepath.Dir(a.cfgPath), saved.OutputDir)
		}
		a.cfgMu.Lock()
		a.cfg = saved
		a.cfgMu.Unlock()
		writeJSON(w, http.StatusOK, saved)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *appState) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/thumbs/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	clip, err := a.store.GetClip(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, clip.ThumbnailPath)
}

func firstPositive(values ...float64) float64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func runFFmpeg(binary string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func randomID() string {
	buf := make([]byte, 16)
	if _, err := crand.Read(buf); err != nil {
		return fmt.Sprintf("clip-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
