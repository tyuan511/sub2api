package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// This opt-in local integration test uses a fixture response, never a paid model.
func TestImageStudioAsyncStoresAndDownloadsFromS3(t *testing.T) {
	endpoint := os.Getenv("IMAGE_STUDIO_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("local object storage is not configured for this test")
	}
	storage, err := repository.NewS3ImageStorage(context.Background(), &config.ImageStorageConfig{
		Endpoint: endpoint, Region: "us-east-1", Bucket: os.Getenv("IMAGE_STUDIO_TEST_S3_BUCKET"),
		AccessKeyID: os.Getenv("IMAGE_STUDIO_TEST_S3_ACCESS_KEY"), SecretAccessKey: os.Getenv("IMAGE_STUDIO_TEST_S3_SECRET"),
		ForcePathStyle: true, PresignExpiry: 1,
	})
	require.NoError(t, err)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, service.NewImageResultUploader(storage, "integration-tests/", 0, nil), time.Hour, time.Minute)
	const fixture = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII="
	h := &AsyncImageHandler{tasks: tasks, execute: func(_ string, c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"b64_json": fixture}}})
	}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true}})
	})
	router.POST("/v1/images/generations/async", h.Submit)
	router.GET("/v1/images/tasks/:task_id", h.Get)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-2","prompt":"test fixture"}`))
	req.Header.Set("Content-Type", "application/json")
	accepted := httptest.NewRecorder()
	router.ServeHTTP(accepted, req)
	require.Equal(t, http.StatusAccepted, accepted.Code)
	var task service.ImageTask
	require.NoError(t, json.Unmarshal(accepted.Body.Bytes(), &task))
	var completed *service.ImageTask
	require.Eventually(t, func() bool {
		completed, err = tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, task.ID)
		return err == nil && completed.Status != service.ImageTaskStatusProcessing
	}, 15*time.Second, 20*time.Millisecond)
	require.Equal(t, service.ImageTaskStatusCompleted, completed.Status)
	require.NotContains(t, string(completed.Result), "b64_json")
	require.True(t, strings.HasPrefix(completed.ImageURL, endpoint+"/"))
	poll := httptest.NewRecorder()
	router.ServeHTTP(poll, httptest.NewRequest(http.MethodGet, "/v1/images/tasks/"+task.ID, nil))
	require.Equal(t, http.StatusOK, poll.Code)
	require.NotContains(t, poll.Body.String(), fixture)
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(completed.ImageURL)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "image/png", response.Header.Get("Content-Type"))
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	want, err := base64.StdEncoding.DecodeString(fixture)
	require.NoError(t, err)
	require.Equal(t, want, data)
	if path := os.Getenv("IMAGE_STUDIO_TEST_RESULT_FILE"); path != "" {
		artifact, err := json.Marshal(map[string]string{"url": completed.ImageURL})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, artifact, 0600))
	}
}

func TestImageStudioStatus(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/image-studio/status", nil)
		if authenticated {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		}
		(&AsyncImageHandler{}).ImageStudioStatus(c)
		if authenticated {
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"available":false`)
		} else {
			require.Equal(t, http.StatusUnauthorized, recorder.Code)
		}
	}
}
