package main

import (
	"runtime/debug"

	"github.com/drkilla/legacy-map/cmd"
)

var version = "dev"

func main() {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}
	cmd.SetVersion(version)
	cmd.Execute()
}
