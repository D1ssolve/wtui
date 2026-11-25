package task

import (
	"slices"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/logutil"
)

// redactReleaseDiagnostics copies only what must change for the manifest write.
func redactReleaseDiagnostics(release domain.Release) domain.Release {
	release.Error = redactReleaseError(release.Error)
	release.Services = slices.Clone(release.Services)
	for i := range release.Services {
		release.Services[i].Error = redactReleaseError(release.Services[i].Error)
	}
	return release
}

func redactReleaseError(diagnostic *domain.ReleaseError) *domain.ReleaseError {
	if diagnostic == nil {
		return nil
	}
	safe := *diagnostic
	safe.Message = logutil.RedactURLUserinfo(safe.Message)
	safe.Cause = logutil.RedactURLUserinfo(safe.Cause)
	return &safe
}
