package main

import (
	"fmt"
	"log/slog"
	"os"

	"leblanc.io/open-go-base/appconf"
	"leblanc.io/open-go-base/logx"
)

// appName and version identify the build. version is injected by the Makefile
// through -ldflags, so --version reports the git tag the binary was cut from.
const appName = "OpenGoPaste"

var version = "develop"

// PasteConfig carries what is specific to this service. The env tags hold no
// prefix: it is added once, on the Config field below, the way appconf expects
// its own fragments to be composed.
type PasteConfig struct {
	DatasFolder  string `yaml:"datas_folder"  env:"DATAS_FOLDER"  env-description:"Folder holding the stored pastes"`
	AssetsFolder string `yaml:"assets_folder" env:"ASSETS_FOLDER" env-description:"Folder overriding the embedded assets"`
	CustomCss    string `yaml:"custom_css"    env:"CUSTOM_CSS"    env-description:"Extra stylesheet, absolute url or /static/... path"`
	Cron         string `yaml:"cron"          env:"CRON"          env-default:"0 0 0 * * *" env-description:"Expired paste cleanup schedule (CRON with seconds)"`
	// The available languages are deduced from the catalogues embedded in the
	// binary; this is only the fallback when the request matches none of them.
	DefaultLanguage string `yaml:"default_language" env:"DEFAULT_LANGUAGE" env-default:"en" env-description:"Fallback language (BCP 47 tag, e.g. en, fr)"`
}

// Config is the whole configuration of the service, read once at startup.
type Config struct {
	Web   appconf.Web     `yaml:"web"     env-prefix:"OPEN_GO_PASTE_"`
	Log   appconf.Logging `yaml:"logging" env-prefix:"OPEN_GO_PASTE_"`
	Paste PasteConfig     `yaml:"paste"   env-prefix:"OPEN_GO_PASTE_"`
}

// config is resolved once by LoadConfig. Every setting used to be read with
// os.Getenv at the point of use, which meant three separate lookups of the
// custom stylesheet, one of them on every single page render.
//
// It is a package level value rather than a parameter threaded through the call
// chain: the views, the storage and the scheduler are already reached through
// package level state, and passing it down to View.Render would be a much wider
// change than the one this refactor is about.
var config Config

// LoadConfig reads the configuration from config.yaml when the file exists, from
// the environment otherwise, wires the --help and --version flags, and installs
// the logger the configuration describes. It exits on a malformed
// configuration: a service that cannot tell where its pastes live must not
// start.
func LoadConfig() {
	appconf.MustLoad(&config, appconf.Options{Name: appName, Version: version})

	// The logger belongs here rather than in main: everything past this point,
	// the deprecation notice included, is entitled to log.
	slog.SetDefault(logx.New(config.Log))

	config.Web.Host = hostFromDeprecatedEnv(config.Web.Host)
}

// hostFromDeprecatedEnv keeps OPEN_GO_PASTE_SERVER working now that appconf.Web
// names the setting HOST. Dropping the old name silently would rebind a service
// listening on 0.0.0.0 to the loopback default on upgrade, which no operator
// would see until the service stopped answering.
//
// The choice is made on the presence of the variables, not on their values:
// once appconf has applied env-default, an explicit 127.0.0.1 and an absent
// HOST are indistinguishable.
//
// Remove with the next major version.
func hostFromDeprecatedEnv(host string) string {
	deprecated, isSet := os.LookupEnv("OPEN_GO_PASTE_SERVER")
	if !isSet || "" == deprecated {
		return host
	}

	if _, isSet := os.LookupEnv("OPEN_GO_PASTE_HOST"); isSet {
		// Both are set, the current name wins. Said out loud because one of the
		// two values is being thrown away.
		deprecationNotice("OPEN_GO_PASTE_SERVER is ignored, OPEN_GO_PASTE_HOST takes precedence")
		return host
	}

	deprecationNotice("OPEN_GO_PASTE_SERVER is deprecated, rename it to OPEN_GO_PASTE_HOST")

	return deprecated
}

// deprecationNotice writes straight to stderr instead of going through the
// logger. The default log level is "error": a deprecation warning routed to the
// logger would be invisible to exactly the operators who still need to act on
// it, and the variable would never get renamed.
func deprecationNotice(message string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", appName, message)
}
