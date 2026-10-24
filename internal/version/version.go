package version

const Name = "salad-bzminer-worker"

// These values are overridden at build time with -ldflags -X.
var (
	Version        = "dev"
	Revision       = "unknown"
	BuildDate      = "unknown"
	BzMinerVersion = "v100.50"
)
