package displaystatus

import "testing"

func TestActionableCaptionsAndRecovery(t *testing.T) {
	s := New()
	s.Select(0, "A")
	s.Select(1, "B")
	s.Bridge("lookup bridge.local: no such host")
	if s.Captions()[0] != DNS || s.Captions()[1] != DNS {
		t.Fatal(s.Captions())
	}
	s.Bridge("")
	s.Observe("A", "ERROR: connection refused")
	if s.Captions()[0] != Refused || s.Captions()[1] != "" {
		t.Fatal(s.Captions())
	}
	s.Observe("A", "started pid 10")
	if s.Captions()[0] == "" {
		t.Fatal("retry hid error")
	}
	s.Observe("A", "eufy-output pid=10 valid=0 monoNs=123")
	if s.Captions()[0] != "" {
		t.Fatal("new output failed toclear")
	}
	s.Observe("B", "ERROR: Not Found (404)")
	if s.Captions()[1] != Missing {
		t.Fatal(s.Captions())
	}
	s.Select(1, "C")
	if s.Captions()[1] != "" {
		t.Fatal("replacement inherited othercameraerror")
	}
}
func TestErrorsNeverExposePrivateURL(t *testing.T) {
	got := Classify("ERROR: failed rtsp://user:secret@private/camera")
	if got != Stream {
		t.Fatal(got)
	}
}

func TestComposerErrorsAndOutputRecoveryMapToCorrectTile(t *testing.T) {
	s := New()
	s.Select(0, "FRONT")
	s.Select(1, "GARAGE")
	s.Observe("wall", "eufytile tile1: decoder stopped mode=0 hardware-init=0 reason=connection refused")
	if s.Captions()[0] != "" || s.Captions()[1] != Refused {
		t.Fatal(s.Captions())
	}
	s.Observe("wall", "eufytile tile1: output-ready")
	if s.Captions()[1] != "" {
		t.Fatal("recoveredcomposererrorpersisted")
	}
}
func TestAuthenticationCaptionsClearWhenReady(t *testing.T) {
	s := New()
	s.Select(0, "A")
	for _, state := range []string{"require_2fa", "require_captcha", "pending", "reauth"} {
		s.Bridge("auth: " + state)
		if s.Captions()[0] == "" || s.Captions()[0] == Stream {
			t.Fatalf("missingauthcaption %s", state)
		}
	}
	s.Bridge("auth: ready")
	if s.Captions()[0] != "" {
		t.Fatal("readydidnotclear")
	}
}

func TestRendererErrorVisibleOnAllTilesUntilRealCameraOutput(t *testing.T) {
	s := New()
	s.Select(0, "A")
	s.Select(1, "B")
	s.Observe("wall", "ERROR: compositor sink failed")
	if s.Captions()[0] == "" || s.Captions()[1] == "" {
		t.Fatal("wallerrorinvisible")
	}
	s.Observe("wall", "eufytile caption0: output-ready")
	if s.Captions()[0] == "" {
		t.Fatal("captionoutputmaskedvideofailure")
	}
	s.Observe("wall", "eufytile tile0: output-ready")
	if s.Captions()[0] != "" || s.Captions()[1] != "" {
		t.Fatal("real output failed toclear renderererror")
	}
}

func TestBoundedComposerConfigurationFailuresAreVisible(t *testing.T) {
	for _, line := range []string{"eufytile tile0: invalid decoder pipeline: unknown element", "eufytile tile0: pipeline requires appsink name=tile_output", "eufytile tile0: invalid configuration: bad file"} {
		s := New()
		s.Select(0, "A")
		s.Observe("wall", line)
		if s.Captions()[0] == "" {
			t.Fatalf("invisiblefailure: %s", line)
		}
	}
}
