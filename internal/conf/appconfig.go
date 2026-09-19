package conf

var setVersion = "0.1.0"
var setCommit = "unknown"

type AppInfo struct {
	Name      string
	Version   string
	Commit    string
	EnvPrefix string
}

var App = AppInfo{
	Name:      "neoserver",
	Version:   setVersion,
	Commit:    setCommit,
	EnvPrefix: "NEOSRV",
}
