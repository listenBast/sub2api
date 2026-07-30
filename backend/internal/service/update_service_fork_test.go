package service

import (
	"context"
	"path/filepath"
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

func TestGitHubAssetDownloadURLPrefersAPIURL(t *testing.T) {
	asset := GitHubAsset{
		APIURL:             "https://api.github.com/repos/listenBast/sub2api/releases/assets/1",
		BrowserDownloadURL: "https://github.com/listenBast/sub2api/releases/download/v0.1.0/asset.tar.gz",
	}
	if got := githubAssetDownloadURL(asset); got != asset.APIURL {
		t.Fatalf("expected API asset URL, got %q", got)
	}

	asset.APIURL = ""
	if got := githubAssetDownloadURL(asset); got != asset.BrowserDownloadURL {
		t.Fatalf("expected browser download fallback, got %q", got)
	}
}

func TestReleaseAssetNameDoesNotComeFromAPIURL(t *testing.T) {
	asset := Asset{
		Name:        "sub2api_0.1.2_linux_amd64.tar.gz",
		DownloadURL: "https://api.github.com/repos/listenBast/sub2api/releases/assets/494832126",
	}

	name, err := safeReleaseAssetName(asset.Name)
	if err != nil {
		t.Fatalf("safe release asset name: %v", err)
	}
	if name != asset.Name {
		t.Fatalf("expected metadata asset name %q, got %q", asset.Name, name)
	}
	if filepath.Base(asset.DownloadURL) == name {
		t.Fatalf("test setup must use an API URL whose basename differs from the asset name")
	}
}

func TestReleaseAssetNameRejectsPaths(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../sub2api.tar.gz", `dir\sub2api.zip`} {
		if _, err := safeReleaseAssetName(name); err == nil {
			t.Fatalf("expected unsafe asset name %q to be rejected", name)
		}
	}
}
