package assistant

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/ChinmayGit8765/lucidbench/internal/prefs"
)

// Bot is a named, saved agent the user can talk to or task: a provider (and
// optionally an account profile and model), a persona that becomes its
// system prompt, and the actions it may propose.
type Bot struct {
	ID       string `json:"id" yaml:"id"`
	Name     string `json:"name" yaml:"name"`
	Provider string `json:"provider" yaml:"provider"`
	Profile  string `json:"profile,omitempty" yaml:"profile,omitempty"`
	Model    string `json:"model,omitempty" yaml:"model,omitempty"`
	Persona  string `json:"persona" yaml:"persona"`
	// AllowedActions limits the catalog for this bot; every kind when made.
	AllowedActions []string `json:"allowed_actions" yaml:"allowed_actions"`
	// Avatar is an emoji, or "sprite:<slot>" for a theme sprite.
	Avatar string `json:"avatar,omitempty" yaml:"avatar,omitempty"`
	// Source says where an imported bot came from, e.g. "claude agents".
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
}

// Limits on a bot.
const (
	MaxBots    = 100
	MaxPersona = 8000
	MaxBotFile = 32 << 10
)

// SpriteSlots are the theme sprite slots an avatar may name.
var SpriteSlots = []string{"loading", "working", "thinking", "success", "failure", "sleeping", "empty", "celebrate"}

var (
	botIDRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	profileRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// CheckBot validates a bot and tidies its fields.
func CheckBot(b *Bot) error {
	b.Name, b.Provider, b.Model, b.Profile, b.Avatar = oneLine(b.Name), strings.TrimSpace(b.Provider), strings.TrimSpace(b.Model), strings.TrimSpace(b.Profile), strings.TrimSpace(b.Avatar)
	b.Persona = strings.TrimSpace(b.Persona)
	switch {
	case !botIDRE.MatchString(b.ID):
		return fmt.Errorf("%w: the bot id must be 1-40 lowercase letters, digits and dashes", ErrBadRequest)
	case b.Name == "" || len(b.Name) > MaxName:
		return fmt.Errorf("%w: the name must be 1-%d characters", ErrBadRequest, MaxName)
	case !slices.Contains(Providers, b.Provider):
		return fmt.Errorf("%w: provider must be claude, codex or grok", ErrBadRequest)
	case b.Model != "" && !modelRE.MatchString(b.Model):
		return fmt.Errorf("%w: model %q is not a model name", ErrBadRequest, b.Model)
	case b.Profile != "" && !profileRE.MatchString(b.Profile):
		return fmt.Errorf("%w: invalid profile name", ErrBadRequest)
	case len(b.Persona) > MaxPersona:
		return fmt.Errorf("%w: the persona is longer than %d characters", ErrBadRequest, MaxPersona)
	}
	if b.AllowedActions == nil {
		b.AllowedActions = Kinds()
	}
	seen := map[string]bool{}
	var acts []string
	for _, a := range b.AllowedActions {
		if !known(a) {
			return fmt.Errorf("%w: %q is not an action (the catalog is %s)", ErrBadRequest, a, strings.Join(Kinds(), ", "))
		}
		if !seen[a] {
			seen[a] = true
			acts = append(acts, a)
		}
	}
	if acts == nil {
		acts = []string{}
	}
	b.AllowedActions = acts
	if b.Avatar != "" {
		if slot, ok := strings.CutPrefix(b.Avatar, "sprite:"); ok {
			if !slices.Contains(SpriteSlots, slot) {
				return fmt.Errorf("%w: sprite avatar must be one of %s", ErrBadRequest, strings.Join(SpriteSlots, ", "))
			}
		} else if utf8.RuneCountInString(b.Avatar) > 8 || strings.ContainsAny(b.Avatar, "<>&\"'`/\\") {
			return fmt.Errorf("%w: the avatar is an emoji or sprite:<slot>", ErrBadRequest)
		}
	}
	if len(b.Source) > 80 {
		b.Source = b.Source[:80]
	}
	return nil
}

func (s *Service) botPath(id string) (string, error) {
	if !botIDRE.MatchString(id) {
		return "", fmt.Errorf("%w: no bot %q", ErrNotFound, id)
	}
	if s.BotsDir == "" {
		return "", fmt.Errorf("%w: bots are not available", ErrNotFound)
	}
	return filepath.Join(s.BotsDir, id+".yaml"), nil
}

// Bots lists the saved bots by name. Files that do not read as a bot are
// skipped.
func (s *Service) Bots() []Bot {
	out := []Bot{}
	if s.BotsDir == "" {
		return out
	}
	files, _ := filepath.Glob(filepath.Join(s.BotsDir, "*.yaml"))
	for _, f := range files {
		b, err := s.Bot(strings.TrimSuffix(filepath.Base(f), ".yaml"))
		if err == nil {
			out = append(out, *b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Bot reads one bot.
func (s *Service) Bot(id string) (*Bot, error) {
	p, err := s.botPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: no bot %q", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBotFile {
		return nil, fmt.Errorf("%w: bot %s is larger than %d KB", ErrBadRequest, id, MaxBotFile>>10)
	}
	var b Bot
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("%w: bot %s: %v", ErrBadRequest, id, err)
	}
	b.ID = id
	if err := CheckBot(&b); err != nil {
		return nil, err
	}
	return &b, nil
}

// SaveBot writes a bot, made or changed.
func (s *Service) SaveBot(b Bot) (*Bot, error) {
	if err := CheckBot(&b); err != nil {
		return nil, err
	}
	p, err := s.botPath(b.ID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(p); err != nil && len(s.Bots()) >= MaxBots {
		return nil, fmt.Errorf("%w: at most %d bots", ErrBadRequest, MaxBots)
	}
	data, err := yaml.Marshal(b)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.BotsDir, 0o755); err != nil {
		return nil, err
	}
	if err := prefs.WriteAtomic(p, data); err != nil {
		return nil, err
	}
	return &b, nil
}

// DeleteBot removes a bot.
func (s *Service) DeleteBot(id string) error {
	p, err := s.botPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: no bot %q", ErrNotFound, id)
	} else if err != nil {
		return err
	}
	return nil
}

// FreeBotID returns an unused bot id based on name.
func (s *Service) FreeBotID(name string) string {
	base := Slug(name)
	if len(base) > 36 {
		base = strings.TrimRight(base[:36], "-")
	}
	id := base
	for i := 2; i < 1000; i++ {
		if p, err := s.botPath(id); err == nil {
			if _, err := os.Stat(p); err != nil {
				return id
			}
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return base + "-" + randHex(2)
}
