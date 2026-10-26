package main

import (
	"runsten/internal/api"
	"runsten/internal/release"
)

// latestRelease is the API's Releases: the checker's newer release.
type latestRelease struct{ c *release.Checker }

func (l latestRelease) Update() (api.Release, bool) {
	r, ok := l.c.Update()
	return api.Release{Version: r.Version, URL: r.URL}, ok
}
