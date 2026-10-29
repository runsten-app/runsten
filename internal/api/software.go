package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// updateStatusJSON is where an update of the software stands.
type updateStatusJSON string

// Schema lists the statuses of an update.
func (updateStatusJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.UpdateAvailable, core.UpdateScheduled, core.UpdateDownloading, core.UpdateWaitingWiFi,
		core.UpdateInstalling, core.UpdateUnknown)
}

// softwareUpdateJSON is the update waiting for the vehicle's software.
type softwareUpdateJSON struct {
	Status      updateStatusJSON `json:"status" doc:"available: offered, not started. scheduled: set to install at a time. downloading. waiting_wifi: waiting for Wi-Fi to download. installing. unknown: a status Runsten does not know, an update waiting all the same."`
	Version     *string          `json:"version" doc:"The version it installs; null when the vehicle does not tell it." example:"2026.38.3.2"`
	DownloadPct *float64         `json:"download_pct" minimum:"0" maximum:"100" doc:"How much of it is downloaded, in %; null when unknown."`
	InstallPct  *float64         `json:"install_pct" minimum:"0" maximum:"100" doc:"How much of it is installed, in %; null when unknown."`
	DurationS   *int64           `json:"duration_s" minimum:"1" doc:"How long the vehicle expects its installation to take, in seconds; null when it does not say."`
}

func softwareUpdateOf(u core.SoftwareUpdate) softwareUpdateJSON {
	out := softwareUpdateJSON{
		Status: text[core.UpdateStatus, updateStatusJSON](u.Status), DownloadPct: opt(u.DownloadPct, same),
		InstallPct: opt(u.InstallPct, same),
	}
	if u.Version != "" {
		out.Version = &u.Version
	}
	if u.Duration.OK {
		s := int64(u.Duration.V / time.Second)
		out.DurationS = &s
	}
	return out
}

// installedJSON bounds when the vehicle took a version.
type installedJSON struct {
	After  *time.Time `json:"after" pattern:"Z$" doc:"The last reading of the previous version; null for the first version Runsten read, installed at any time before."`
	Before time.Time  `json:"before" pattern:"Z$" doc:"The first reading of the version."`
}

// softwareVersionJSON is a version of the vehicle's software.
type softwareVersionJSON struct {
	ID        string        `json:"id" doc:"Identifies the version for its vehicle: its first reading's time, to the microsecond, which a rebuild of the derived data keeps. Treat it as opaque." example:"2026-10-26T16:29:31.549000Z"`
	Version   string        `json:"version" example:"2026.38.3.1"`
	Installed installedJSON `json:"installed" doc:"When the vehicle took it: between the last reading of the previous version and its own first one."`
}

func softwareVersionOf(v core.SoftwareVersion) softwareVersionJSON {
	return softwareVersionJSON{
		ID: v.DetectedAt.UTC().Format(idFormat), Version: v.Version,
		Installed: installedJSON{After: timeOrNil(v.Installed.After), Before: v.Installed.Before},
	}
}

// listSoftwareVersions reads a page of the vehicle's versions, newest first.
func (s *Server) listSoftwareVersions(ctx context.Context, in *listInput) (*body[page[softwareVersionJSON]], error) {
	return listEvents(ctx, s, in, func(acc string, v Vehicle, q EventQuery) ([]core.SoftwareVersion, error) {
		return s.Reader.ListSoftwareVersions(ctx, acc, v.ID, q) //nolint:wrapcheck // logged by listEvents, never shown
	}, func(v core.SoftwareVersion) EventKey { return EventKey{DetectedAt: v.DetectedAt} },
		func(vs []core.SoftwareVersion, _ AccountLimits) []softwareVersionJSON {
			return each(softwareVersionOf)(vs)
		})
}

// registerSoftware registers the history of a vehicle's software.
func (s *Server) registerSoftware(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listSoftwareVersions", Method: http.MethodGet, Path: "/vehicles/{vehicle}/software-versions",
		Tags: []string{"vehicles"}, Summary: "The versions of a vehicle's software",
		Description: "Each version of the vehicle's software, from when Runsten first read it, newest first: for a " +
			"vehicle whose source tells it (a Tesla), [] for the others. Derived from the readings, as the trips: a " +
			"rebuild of the derived data recomputes them. `from` and `to` keep the versions that may have been " +
			"installed within the period. The version running and the update waiting are in the vehicle's state.",
	}, "A page of versions, newest first.", map[int]string{
		http.StatusBadRequest: errParameter, http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle,
		http.StatusInternalServerError: errInternal,
	}), s.listSoftwareVersions)
}
