package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eufy-wall/internal/config"
)

func contractInventoryPath(name string) string {
	return filepath.Join("..", "..", "..", "config-contract", name)
}

func TestInventoryV1ContractImportsIntoClientSetup(t *testing.T) {
	path := contractInventoryPath("inventory-v1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var exported struct {
		SchemaVersion int       `json:"schema_version"`
		ExportedAt    time.Time `json:"exported_at"`
		BridgeURL     string    `json:"bridge_url"`
		Cameras       []struct {
			SN        string  `json:"sn"`
			StreamKey string  `json:"streamKey"`
			RTSP      string  `json:"rtsp"`
			Codec     *string `json:"codec"`
		} `json:"cameras"`
	}
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.SchemaVersion != 1 || exported.ExportedAt.IsZero() || exported.BridgeURL != "http://bridge.local:3000" || len(exported.Cameras) != 3 {
		t.Fatalf("invalid contract envelope: %+v", exported)
	}
	if exported.Cameras[1].StreamKey != "entry/side door #1" || exported.Cameras[1].RTSP != "rtsp://bridge.local:8554/entry%2Fside%20door%20%231" || exported.Cameras[2].Codec != nil {
		t.Fatalf("lost unusual stream key or nullable codec: %+v", exported.Cameras)
	}
	cameras, err := fetchSetupCameras(context.Background(), "", path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cameras) != 3 || cameras[0].Codec != "h264" || cameras[0].Mode != "always" || !cameras[0].Powered || cameras[1].Codec != "h265" || cameras[1].Mode != "on_demand" || cameras[1].Powered || cameras[2].Codec != "" || cameras[2].Mode != "on_motion" || cameras[2].Powered {
		t.Fatalf("client changed inventory semantics: %+v", cameras)
	}
	selected := []string{cameras[0].SN, cameras[1].SN, cameras[2].SN}
	draft, err := setupYAML(setupAnswers{BridgeURL: exported.BridgeURL, RTSPBase: "rtsp://bridge.local:8554", Cameras: selected, Template: "four"}, cameras)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := config.Parse(draft)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Tiles) != 3 || parsed.Tiles[0].Codec != "" || parsed.Tiles[1].Codec != "h265" || parsed.Tiles[2].Codec != "" {
		t.Fatalf("codec hints in generated client config: %+v", parsed.Tiles)
	}
}

func TestInventoryContractRejectsUnsupportedVersionAndDuplicateSerial(t *testing.T) {
	for _, tc := range []struct{ file, message string }{
		{"inventory-v2-unsupported.json", "schema_version 2 is unsupported"},
		{"inventory-v1-duplicate-serial.json", "duplicate serial"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			_, err := fetchSetupCameras(context.Background(), "", contractInventoryPath(tc.file))
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("error=%v, want %q", err, tc.message)
			}
		})
	}
}
