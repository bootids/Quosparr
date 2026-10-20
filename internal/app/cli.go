package app

import "slices"

// Command is what the command line asks for.
type Command int

const (
	// CommandRun starts the menu-bar app (the default).
	CommandRun Command = iota
	CommandHelp
	CommandSelfCheck
	CommandQuota
	CommandSettings
)

// HelpText is the --help output. It keeps the Swift app's layout; options
// that are not ported yet are left out rather than advertised.
const HelpText = `Usage: quosparr [options]

Options:
  -h, --help
      Show this help and exit.

  --self-check
      Print redacted local diagnostics and exit. No network.

  --settings
      Open the settings window after launching.

  --quota [--status-file PATH] [--max-age SECONDS] [provider ...]
      Print versioned quota JSON from the status export and exit. No network.
      Requires Export status JSON; max-age defaults to 900 seconds.

`

// ParseArgs picks the command from args (without the program name).
// --help wins over --quota, then --self-check, then --settings. Anything else
// is ignored, so flags a launcher adds never keep the app from starting.
func ParseArgs(args []string) Command {
	switch {
	case slices.Contains(args, "--help") || slices.Contains(args, "-h"):
		return CommandHelp
	case slices.Contains(args, "--quota"):
		return CommandQuota
	case slices.Contains(args, "--self-check"):
		return CommandSelfCheck
	case slices.Contains(args, "--settings"):
		return CommandSettings
	}
	return CommandRun
}

// LaunchActions routes launch requests to the existing UI entry points.
type LaunchActions struct {
	Ready        func()
	OpenPanel    func()
	OpenSettings func()
	FirstLaunch  bool
}

// InitialReady initializes the tray before opening a requested window.
// A fresh installation opens settings once; later normal launches stay in the tray.
func (a LaunchActions) InitialReady(command Command) {
	a.Ready()
	if command == CommandSettings || a.FirstLaunch {
		a.OpenSettings()
	}
}

// SecondInstance receives MyGo's arguments without the executable name.
func (a LaunchActions) SecondInstance(args []string, _ string) {
	if ParseArgs(args) == CommandSettings {
		a.OpenSettings()
		return
	}
	a.OpenPanel()
}
