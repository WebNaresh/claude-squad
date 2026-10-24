package session

import "testing"

// A gg:video issue is a video request: it always waits, so neither the
// picker nor the auto runner starts a session on it (glitchgrab #642).
func TestVideoRequestWaits(t *testing.T) {
	var video, plain Issue
	video.Labels = append(video.Labels, struct {
		Name string `json:"name"`
	}{"enhancement"}, struct {
		Name string `json:"name"`
	}{"gg:video"})
	plain.Labels = append(plain.Labels, struct {
		Name string `json:"name"`
	}{"gg:feature"})
	video.Video, plain.Video = isVideoRequest(video), isVideoRequest(plain)
	if !video.Waits() || video.WaitReason() != "video request, made in GlitchRecord" {
		t.Errorf("gg:video issue: Waits() = %v, reason %q", video.Waits(), video.WaitReason())
	}
	if plain.Waits() {
		t.Error("gg:feature issue waits; want free")
	}
}
