package service

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

type StudioLegacyResult struct {
	Status string              `json:"status"`
	Images []map[string]string `json:"images"`
	Error  string              `json:"error,omitempty"`
}

func (s *ImageStudioService) Legacy(ctx context.Context, userID int64, id string) (*StudioLegacyResult, error) {
	if s.legacyTasks == nil {
		return nil, ErrImageTaskNotFound
	}
	task, err := s.legacyTasks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID {
		return nil, ErrImageTaskNotFound
	}
	out := &StudioLegacyResult{Status: task.Status, Images: []map[string]string{}}
	var failure struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(task.Error, &failure)
	out.Error = failure.Message
	var result struct {
		Data []struct {
			URL    string `json:"url"`
			Base64 string `json:"b64_json"`
		} `json:"data"`
	}
	_ = json.Unmarshal(task.Result, &result)
	if err := s.syncStorage(ctx); err != nil {
		return nil, err
	}
	profiles, err := s.repo.ListStorage(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range result.Data {
		link := item.URL
		if link == "" && item.Base64 != "" {
			link = "data:image/png;base64," + item.Base64
		}
		if original, e := url.Parse(item.URL); e == nil && original.Host != "" {
			for _, profile := range profiles {
				storage, e := s.objectStorage(ctx, &profile)
				if e != nil {
					continue
				}
				probe, e := storage.URL(ctx, "__studio_probe__")
				if e != nil {
					continue
				}
				base, e := url.Parse(probe)
				if e != nil {
					continue
				}
				prefix := strings.TrimSuffix(base.Path, "__studio_probe__")
				if original.Scheme != base.Scheme || original.Host != base.Host || !strings.HasPrefix(original.Path, prefix+profile.Config.Prefix) {
					continue
				}
				// The URL came from a server-side task belonging to this user, not from
				// an arbitrary client-supplied path. Renew it using its original profile.
				key := strings.TrimPrefix(original.Path, prefix)
				if fresh, e := storage.URL(ctx, key); e == nil {
					link = fresh
				}
				break
			}
		}
		if link != "" {
			out.Images = append(out.Images, map[string]string{"url": link})
		}
	}
	return out, nil
}
