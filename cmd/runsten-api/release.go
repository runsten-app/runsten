package main

import (
	"time"

	"runsten/internal/api"
	"runsten/internal/release"
)

// latestRelease is the API's Releases: the checker's newer release.
type latestRelease struct{ c *release.Checker }

func (l latestRelease) Latest() (time.Time, api.Release, bool) {
	r, ok := l.c.Update()
	return l.c.CheckedAt(), api.Release{Version: r.Version, URL: r.URL}, ok
}
