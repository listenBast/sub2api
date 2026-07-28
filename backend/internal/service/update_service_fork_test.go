package service

import (
	"context"
	"testing"
	"time"
)

type forkReleaseClient struct {
	repo string
}

func (c *forkReleaseClient) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	c.repo = repo
	return &GitHubRelease{TagName: "v1.0.0"}, nil
}

func (c *forkReleaseClient) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return nil, nil
}

func (c *forkReleaseClient) DownloadFile(context.Context, string, string, int64) error { return nil }
func (c *forkReleaseClient) FetchChecksumFile(context.Context, string) ([]byte, error) {
	return nil, nil
}

type emptyUpdateCache struct{}

func (emptyUpdateCache) GetUpdateInfo(context.Context) (string, error)              { return "", context.Canceled }
func (emptyUpdateCache) SetUpdateInfo(context.Context, string, time.Duration) error { return nil }

func TestUpdateServiceChecksForkRepository(t *testing.T) {
	client := &forkReleaseClient{}
	service := NewUpdateService(emptyUpdateCache{}, client, "0.9.0", "release")
	_, err := service.CheckUpdate(context.Background(), true)
	if err != nil {
		t.Fatalf("check update: %v", err)
	}
	if client.repo != "listenBast/sub2api" {
		t.Fatalf("expected fork repository, got %q", client.repo)
	}
}
