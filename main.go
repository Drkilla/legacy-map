package main

import "github.com/drkilla/legacy-map/cmd"

var version = "dev"

func main() {
	cmd.SetVersion(version)
	cmd.Execute()
}
