package settings

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/432539/gpt2api/internal/videogen"
	"github.com/gin-gonic/gin"
)

func TestVideoGenRequestChannelReadsMultipartForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader("--x\r\n" +
		`Content-Disposition: form-data; name="channel_type"` + "\r\n\r\n" +
		videogen.ChannelAPIYIWan27 + "\r\n" +
		"--x--\r\n")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/settings/videogen-generate-test", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := videoGenRequestChannel(c); got != videogen.ChannelAPIYIWan27 {
		t.Fatalf("channel type = %q, want %q", got, videogen.ChannelAPIYIWan27)
	}
}

func TestVideoGenSupportsReferenceVideoIncludesSeedance(t *testing.T) {
	for _, channelType := range []string{
		videogen.ChannelAPIYISeedance,
		videogen.ChannelAPIYIWan27,
		videogen.ChannelAPIYIHappyHorse,
	} {
		if !videoGenSupportsReferenceVideo(channelType) {
			t.Fatalf("channel %q should support reference video", channelType)
		}
	}
	if videoGenSupportsReferenceVideo(videogen.ChannelEchoon) {
		t.Fatal("echoon should not support reference video")
	}
}
