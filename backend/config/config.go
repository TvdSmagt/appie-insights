package config

// Server holds the file-system paths shared across all HTTP handler groups.
type Server struct {
	DBPath     string
	ConfigPath string
	DataDir    string
}
