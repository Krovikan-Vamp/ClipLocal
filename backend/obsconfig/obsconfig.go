package obsconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"cliplocal/backend/config"
)

func GenerateOBSConfig(obsProfileDir string, cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}

	profileName := "ClipLocal"
	basicDir := filepath.Join(obsProfileDir, "basic")
	profilesDir := filepath.Join(basicDir, "profiles", profileName)
	scenesDir := filepath.Join(basicDir, "scenes")

	for _, dir := range []string{obsProfileDir, basicDir, profilesDir, scenesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	basicINI := fmt.Sprintf(`[General]
Name=%s

[Output]
Mode=Advanced
RecType=Standard
RecFormat2=mkv
VBitrate=0
RecEncoder=obs_x264

[SimpleOutput]
StreamEncoder=x264
FilePath=%s
RecFormat2=mkv
RecQuality=Small
RecRB=true
RecRBTime=%d

[Video]
BaseCX=1920
BaseCY=1080
OutputCX=1920
OutputCY=1080
FPSType=0
FPSCommon=60
`, profileName, cfg.OutputDir, cfg.ReplayBufferSeconds)
	if err := os.WriteFile(filepath.Join(profilesDir, "basic.ini"), []byte(basicINI), 0o644); err != nil {
		return err
	}

	sceneCollection := map[string]interface{}{
		"name":                  profileName,
		"current_program_scene": "ClipLocal",
		"scene_order":           []string{"ClipLocal"},
		"sources": []map[string]interface{}{
			{
				"name":     "ClipLocal",
				"id":       "scene",
				"settings": map[string]interface{}{},
			},
			{
				"name": "Windows Capture",
				"id":   "window_capture",
				"settings": map[string]interface{}{
					"capture_method": "windows_graphics_capture",
				},
			},
		},
	}
	sceneBytes, err := json.MarshalIndent(sceneCollection, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(scenesDir, profileName+".json"), sceneBytes, 0o644); err != nil {
		return err
	}

	globalINI := fmt.Sprintf(`[Basic]
Profile=%s
SceneCollection=%s
`, profileName, profileName)
	if err := os.WriteFile(filepath.Join(obsProfileDir, "global.ini"), []byte(globalINI), 0o644); err != nil {
		return err
	}

	websocketConfig := fmt.Sprintf(`{
  "server_enabled": true,
  "server_port": %d,
  "server_password": %q
}
`, cfg.OBS.Port, cfg.OBS.Password)
	return os.WriteFile(filepath.Join(obsProfileDir, "obs-websocket.json"), []byte(websocketConfig), 0o644)
}
