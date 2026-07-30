package pipeline

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cliplocal/backend/store"
	"github.com/fsnotify/fsnotify"
)

type Pipeline struct {
	watchDir   string
	outputDir  string
	ffmpegPath string
	store      *store.Store
	apmFn      func() int
	titleFn    func() string

	watcher    *fsnotify.Watcher
	stop       chan struct{}
	once       sync.Once
	processing sync.Map
}

func NewPipeline(watchDir, outputDir, ffmpegPath string, store *store.Store, apmFn func() int, titleFn func() string) *Pipeline {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &Pipeline{
		watchDir:   watchDir,
		outputDir:  outputDir,
		ffmpegPath: ffmpegPath,
		store:      store,
		apmFn:      apmFn,
		titleFn:    titleFn,
		stop:       make(chan struct{}),
	}
}

func (p *Pipeline) Start() error {
	if err := os.MkdirAll(p.watchDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(p.outputDir, 0o755); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := watcher.Add(p.watchDir); err != nil {
		_ = watcher.Close()
		return err
	}
	p.watcher = watcher

	go p.watchLoop()
	return nil
}

func (p *Pipeline) Stop() {
	p.once.Do(func() {
		close(p.stop)
		if p.watcher != nil {
			_ = p.watcher.Close()
		}
	})
}

func (p *Pipeline) processFile(path string) {
	if !isClipCandidate(path) {
		return
	}
	if _, loaded := p.processing.LoadOrStore(path, struct{}{}); loaded {
		return
	}
	defer p.processing.Delete(path)

	if err := waitForStableSize(path); err != nil {
		log.Printf("pipeline: file never stabilized %s: %v", path, err)
		return
	}

	clipID := newID()
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if strings.Contains(name, "_processed") || strings.Contains(name, "_thumb") || strings.Contains(name, "_trim") || strings.Contains(name, ".discord") {
		return
	}

	outPath := filepath.Join(p.outputDir, name+"_processed.mp4")
	thumbPath := filepath.Join(p.outputDir, name+"_thumb.jpg")

	if err := p.runFFmpeg("-y", "-i", path, "-c:v", "libx264", "-crf", "23", "-c:a", "aac", "-fs", "24000000", outPath); err != nil {
		log.Printf("pipeline: encode failed for %s: %v", path, err)
		return
	}
	if err := p.runFFmpeg("-y", "-i", outPath, "-vf", "thumbnail,scale=640:-1", "-frames:v", "1", thumbPath); err != nil {
		log.Printf("pipeline: thumbnail failed for %s: %v", outPath, err)
		return
	}

	duration, _ := probeDuration(p.ffmpegPath, outPath)
	info, err := os.Stat(outPath)
	if err != nil {
		log.Printf("pipeline: stat failed for %s: %v", outPath, err)
		return
	}

	title := ""
	if p.titleFn != nil {
		title = p.titleFn()
	}
	clip := store.Clip{
		ID:              clipID,
		Filepath:        outPath,
		ThumbnailPath:   thumbPath,
		CreatedAt:       time.Now().UTC(),
		GameTitle:       title,
		DurationSeconds: duration,
		SizeBytes:       info.Size(),
	}
	if p.apmFn != nil {
		clip.APMAtCapture = p.apmFn()
	}

	if err := p.store.InsertClip(clip); err != nil {
		log.Printf("pipeline: failed to insert clip: %v", err)
	}
}

func (p *Pipeline) runFFmpeg(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("ffmpeg %v failed: %w (%s)", args, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (p *Pipeline) watchLoop() {
	for {
		select {
		case <-p.stop:
			return
		case event, ok := <-p.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Write) {
				go p.processFile(event.Name)
			}
		case err, ok := <-p.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("pipeline watcher error: %v", err)
		}
	}
}

func waitForStableSize(path string) error {
	deadline := time.Now().Add(30 * time.Second)
	var lastSize int64 = -1
	var stableSince time.Time

	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if info.Size() == lastSize {
			if stableSince.IsZero() {
				stableSince = time.Now()
			}
			if time.Since(stableSince) >= 2*time.Second {
				return nil
			}
		} else {
			lastSize = info.Size()
			stableSince = time.Time{}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("file did not stabilize before timeout")
}

func isClipCandidate(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".mkv" || ext == ".mp4"
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := crand.Read(buf); err != nil {
		return fmt.Sprintf("clip-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func probeDuration(ffmpegPath, videoPath string) (float64, error) {
	probe := "ffprobe"
	if ffmpegPath != "" && ffmpegPath != "ffmpeg" {
		dir := filepath.Dir(ffmpegPath)
		candidate := filepath.Join(dir, strings.Replace(filepath.Base(ffmpegPath), "ffmpeg", "ffprobe", 1))
		if _, err := os.Stat(candidate); err == nil {
			probe = candidate
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, probe, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", videoPath)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	var duration float64
	_, err = fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &duration)
	return duration, err
}
