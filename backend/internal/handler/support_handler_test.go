package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteSupportServiceErrorIncludesRateLimitPrompt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/support/1/replies", nil)

	writeSupportServiceError(ctx, infraerrors.TooManyRequests("SUPPORT_MESSAGE_LIMIT_MINUTE", "发送过于频繁，每分钟最多发送 10 条消息，请稍后再试").WithMetadata(map[string]string{
		"retry_after_seconds": "37",
	}))

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "37", recorder.Header().Get("Retry-After"))
	var payload response.Response
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "发送过于频繁，每分钟最多发送 10 条消息，请稍后再试", payload.Message)
}

func TestSupportUploadRequestBodyLimitPrompt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("attachments", "large.png")
	require.NoError(t, err)
	_, err = part.Write(make([]byte, service.SupportMaxRequestBodyBytes))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	router := gin.New()
	router.POST("/support", servermiddleware.RequestBodyLimit(service.SupportMaxRequestBodyBytes), func(c *gin.Context) {
		_, uploadErr := readSupportUploads(c)
		require.Error(t, uploadErr)
		writeSupportUploadError(c, uploadErr)
	})
	request := httptest.NewRequest(http.MethodPost, "/support", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	var payload response.Response
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "单次发送内容和图片总大小不能超过 7 MB", payload.Message)
}
