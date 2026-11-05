// Package version reports build metadata.
//
// Release builds set the values at link time:
//
//	go build -ldflags "-X github.com/arifurrahmanns/envry/internal/version.Version=v1.2.3"
//
// Otherwise they are filled from the build info Go embeds automatically
// (module version for `go install ...@v1.2.3`, VCS revision for local builds).
package version

import "runtime/debug"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if Commit == "none" && len(s.Value) >= 7 {
				Commit = s.Value[:7]
			}
		case "vcs.time":
			if Date == "unknown" {
				Date = s.Value
			}
		}
	}
}
