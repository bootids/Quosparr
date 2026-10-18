package app

import (
	"context"
	"errors"
	"os"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/licoy/quosparr/internal/platform"
	"github.com/licoy/quosparr/internal/skills"
	"github.com/licoy/quosparr/internal/store"
)

var errSkillsUnavailable = errors.New("skills service unavailable")

// installSkillActions wires the skills pages to the scanner, the provider
// config writers, the Quosparr skills trash and the system shell. Deleting
// moves to that trash (the system Trash from another volume); purging
// moves on to the system Trash, so nothing is erased.
func installSkillActions(rt *liveApp, a *SkillActions) {
	a.Query = rt.querySkills
	a.ReadBody = skills.ReadBody
	a.OpenFolder = mygo.Shell.OpenPath
	a.Reveal = mygo.Shell.ShowItemInFolder
	a.Copy = mygo.Clipboard.WriteText
	a.Inspect = skills.Inspect
	// Remote mode keeps the skills page read-only: enabling, deleting,
	// restoring and emptying the trash all write the local config and stay
	// disabled.
	if rt.remoteMode() {
		return
	}
	home := rt.skillsEnv().Home
	trash := skills.NewTrash(platform.RunwayDir(home), home)
	system := mygo.Shell.TrashItem
	a.SetEnabled = rt.setSkillEnabled
	a.Delete = func(s skills.Skill, roots []skills.RootInfo) (bool, error) {
		_, inSystem, err := trash.MoveSkill(s, roots, time.Now(), system)
		return inSystem, err
	}
	a.DeleteLink = func(sum skills.Summary, path string) (bool, error) {
		_, inSystem, err := trash.MoveBrokenLink(sum, path, time.Now(), system)
		return inSystem, err
	}
	a.TrashDir = trash.Dir
	a.ListTrash = trash.List
	a.Restore = trash.Restore
	a.Purge = func(id string) error { return trash.Purge(id, system) }
	a.EmptyTrash = func() (int, error) { return trash.Empty(system) }
}

func (rt *liveApp) skillsEnv() skills.Env {
	env := skills.Env{
		CodexHome: rt.codexFolder(),
		ClaudeDir: rt.claudeFolder(),
		GrokHome:  rt.grokFolder(),
		GOOS:      runtime.GOOS,
		Getenv:    os.Getenv,
		Now:       time.Now(),
	}
	if rt.claudeMod != nil && rt.claudeMod.Store != nil {
		env.Home = rt.claudeMod.Store.HomeDir()
	}
	if env.Home == "" {
		env.Home, _ = os.UserHomeDir()
	}
	return env
}

// querySkills scans all three CLIs so copies in a shared directory
// (~/.agents/skills, ~/.claude/skills) know who else reads them.
func (rt *liveApp) querySkills(ctx context.Context) (map[store.Provider]skills.Summary, error) {
	env := rt.skillsEnv()
	cx, err := skills.ScanCodex(ctx, env)
	if err != nil {
		return nil, err
	}
	cl, err := skills.ScanClaude(ctx, env)
	if err != nil {
		return nil, err
	}
	gk, err := skills.ScanGrok(ctx, env)
	if err != nil {
		return nil, err
	}
	skills.MarkShared(&cx, &cl, &gk)
	return map[store.Provider]skills.Summary{
		store.ProviderCodex:  cx,
		store.ProviderClaude: cl,
		store.ProviderGrok:   gk,
	}, nil
}

// setSkillEnabled writes the provider's own switch: Codex [[skills.config]],
// Claude skillOverrides, Grok [skills].disabled; each backs up first.
func (rt *liveApp) setSkillEnabled(s skills.Skill, on bool) error {
	now := time.Now()
	switch s.Provider {
	case skills.ProviderCodex:
		if rt.codex == nil {
			return errSkillsUnavailable
		}
		return rt.codex.SetSkillEnabledFile(s.File, s.InvokeName, on, now)
	case skills.ProviderClaude:
		if rt.claudeMod == nil || rt.claudeMod.Store == nil {
			return errSkillsUnavailable
		}
		return rt.claudeMod.Store.SetSkillEnabledFile(s.Name, on, now)
	case skills.ProviderGrok:
		if rt.grokMod == nil || rt.grokMod.Store == nil {
			return errSkillsUnavailable
		}
		return rt.grokMod.Store.SetSkillEnabledFile(s.Name, on, now)
	}
	return errSkillsUnavailable
}
