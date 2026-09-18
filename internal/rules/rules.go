// Package rules defines the YAML schema that tells a detector what "Teams is
// in a call" looks like on the wire: which process, which window titles, and
// whether the microphone or camera is held by it.
//
// The package is deliberately pure Go: it parses YAML, compiles regexes, and
// scores an Observation that some other package assembled from the real
// world. It has no platform dependency of its own, so it can be fixture-tested
// on any OS and is called by the platform-specific detector rather than the
// other way around.
//
// Tuning philosophy: a wrong "active" is worse than a missed one. Process
// presence alone is weak evidence and is never enough by itself; a window
// title match is treated as strong evidence (it is usually a distinctive,
// product-specific string); the microphone is never sufficient alone, because
// music and personal calls hold it too. Weights are chosen so that only a
// combination of signals crosses the active threshold, except where a single
// signal (a meeting window title) is distinctive enough to stand on its own.
package rules

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/crs2007/callmqtt/internal/model"
)

// defaultRulesYAML is CallMQTT's shipped rule set, tuned against the
// fixtures in testdata/probe/*.txt. It is not user-configurable: unlike
// config.yaml, a wrong pattern here needs a new capture and a test, not a
// runtime edit, so it is compiled into the binary rather than read from a
// path.
//
//go:embed rules.yaml
var defaultRulesYAML []byte

// Default parses and compiles CallMQTT's shipped rule set.
func Default() (*Config, error) {
	return Load(defaultRulesYAML)
}

// Weights controls how much each kind of evidence contributes to an app's
// confidence score. The sum need not be 1; Evaluate clamps the total.
type Weights struct {
	// Process is the weight given to the app's process simply being present
	// (a visible window belonging to one of ProcessNames). Weak evidence:
	// having Teams open says nothing about being in a meeting.
	Process float64 `yaml:"process"`

	// Window is the weight given to at least one of that process's windows
	// matching WindowIncludeRegex and none of its (the same window's)
	// WindowExcludeRegex.
	Window float64 `yaml:"window"`

	// Mic is the weight given to the microphone being held by this app, as
	// determined by MicProcessRegex matching an entry in the mic-in-use list.
	Mic float64 `yaml:"mic"`

	// Cam is the weight given to the webcam being held by this app, as
	// determined by CamProcessRegex matching an entry in the cam-in-use list.
	// No fixture currently exercises this signal; it exists so a future
	// capture can be wired in without a schema change.
	Cam float64 `yaml:"cam"`
}

// Rule is the on-disk (YAML) description of how to detect one application.
type Rule struct {
	// App is the short name used in DetectionResult.App and in log lines
	// ("teams", "zoom", "slack").
	App string `yaml:"app"`

	// ProcessNames lists the exact process image names (as reported by the
	// OS, e.g. "ms-teams.exe") that belong to this app. Matched
	// case-insensitively.
	ProcessNames []string `yaml:"process_names"`

	// WindowIncludeRegex are patterns tested against a window's title. A
	// window counts toward the Window signal if its owning process is in
	// ProcessNames, its title matches at least one of these, and its title
	// matches none of WindowExcludeRegex.
	WindowIncludeRegex []string `yaml:"window_include_regex"`

	// WindowExcludeRegex are patterns that veto an otherwise-matching window
	// title. This is how a Teams chat or calendar window is kept from ever
	// being read as a meeting.
	WindowExcludeRegex []string `yaml:"window_exclude_regex,omitempty"`

	// MicProcessRegex are patterns tested against each entry of the
	// mic-in-use list (process names, paths, or platform bundle/session
	// identifiers - whatever the OS layer reports). A match attributes the
	// microphone to this app.
	MicProcessRegex []string `yaml:"mic_process_regex,omitempty"`

	// CamProcessRegex is the webcam equivalent of MicProcessRegex.
	CamProcessRegex []string `yaml:"cam_process_regex,omitempty"`

	Weights Weights `yaml:"weights"`
}

// CompiledRule is a Rule with every regex compiled once at load time, so the
// poll loop only ever matches, never compiles.
type CompiledRule struct {
	Rule

	includeRe []*regexp.Regexp
	excludeRe []*regexp.Regexp
	micRe     []*regexp.Regexp
	camRe     []*regexp.Regexp
}

// Config is a loaded, compiled set of rules, one per application.
type Config struct {
	Rules []CompiledRule
}

// WindowObservation is one visible window, as reported by the platform
// window-enumeration layer.
type WindowObservation struct {
	Proc  string
	Title string
}

// Observation is everything a rule needs to score one poll: the visible
// windows, and the raw identifiers of whatever currently holds the
// microphone and webcam.
type Observation struct {
	Windows  []WindowObservation
	MicInUse []string
	CamInUse []string
}

// LoadFile reads, parses and compiles a rules document from disk.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rules: read %s: %w", path, err)
	}
	return Load(data)
}

// Load parses and compiles a rules document from memory.
func Load(data []byte) (*Config, error) {
	var raw struct {
		Rules []Rule `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("rules: parse: %w", err)
	}

	cfg := &Config{Rules: make([]CompiledRule, 0, len(raw.Rules))}
	for i, r := range raw.Rules {
		compiled, err := compile(r)
		if err != nil {
			label := r.App
			if label == "" {
				label = fmt.Sprintf("#%d", i)
			}
			return nil, fmt.Errorf("rules: app %q: %w", label, err)
		}
		cfg.Rules = append(cfg.Rules, compiled)
	}
	return cfg, nil
}

// compile turns one Rule's regex strings into compiled patterns. A bad regex
// is reported as a config-load error with the offending field and pattern
// named, never left to panic later in the poll loop.
func compile(r Rule) (CompiledRule, error) {
	include, err := compileAll("window_include_regex", r.WindowIncludeRegex)
	if err != nil {
		return CompiledRule{}, err
	}
	exclude, err := compileAll("window_exclude_regex", r.WindowExcludeRegex)
	if err != nil {
		return CompiledRule{}, err
	}
	mic, err := compileAll("mic_process_regex", r.MicProcessRegex)
	if err != nil {
		return CompiledRule{}, err
	}
	cam, err := compileAll("cam_process_regex", r.CamProcessRegex)
	if err != nil {
		return CompiledRule{}, err
	}
	return CompiledRule{Rule: r, includeRe: include, excludeRe: exclude, micRe: mic, camRe: cam}, nil
}

func compileAll(field string, patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for i, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("%s[%d] %q: %w", field, i, p, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// hasProcess reports whether any window belongs to one of the rule's
// process names.
func (r CompiledRule) hasProcess(obs Observation) bool {
	for _, w := range obs.Windows {
		if r.ownsProcess(w.Proc) {
			return true
		}
	}
	return false
}

func (r CompiledRule) ownsProcess(proc string) bool {
	for _, name := range r.ProcessNames {
		if strings.EqualFold(proc, name) {
			return true
		}
	}
	return false
}

// matchesWindow reports whether a single window (already known to belong to
// this app) counts as a meeting-window match: it matches at least one
// include pattern and none of the exclude patterns. Exclusion is evaluated
// per window, not per app, so a meeting window and an unrelated chat window
// open at the same time do not veto each other.
func (r CompiledRule) matchesWindow(title string) bool {
	matched := false
	for _, re := range r.includeRe {
		if re.MatchString(title) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	for _, re := range r.excludeRe {
		if re.MatchString(title) {
			return false
		}
	}
	return true
}

// hasWindowMatch reports whether any window owned by this app's process is a
// meeting-window match.
func (r CompiledRule) hasWindowMatch(obs Observation) bool {
	for _, w := range obs.Windows {
		if r.ownsProcess(w.Proc) && r.matchesWindow(w.Title) {
			return true
		}
	}
	return false
}

func matchesAny(patterns []*regexp.Regexp, entries []string) bool {
	for _, entry := range entries {
		for _, re := range patterns {
			if re.MatchString(entry) {
				return true
			}
		}
	}
	return false
}

// Evaluate scores one rule against one observation.
//
// activeThreshold is supplied by the caller (normally
// config.Detection.ActiveThreshold) rather than fixed here, so the same
// compiled rules can be tuned per-deployment without editing YAML.
func (r CompiledRule) Evaluate(obs Observation, activeThreshold float64, now time.Time) model.DetectionResult {
	var score float64
	var reasons []string

	processPresent := r.hasProcess(obs)
	if processPresent {
		score += r.Weights.Process
		reasons = append(reasons, r.App+": process present")
	}

	if r.hasWindowMatch(obs) {
		score += r.Weights.Window
		reasons = append(reasons, r.App+": meeting window title matched")
	}

	if len(r.micRe) > 0 && matchesAny(r.micRe, obs.MicInUse) {
		score += r.Weights.Mic
		reasons = append(reasons, r.App+": microphone in use")
	}

	if len(r.camRe) > 0 && matchesAny(r.camRe, obs.CamInUse) {
		score += r.Weights.Cam
		reasons = append(reasons, r.App+": webcam in use")
	}

	confidence := model.ClampConfidence(score)

	// Not having seen the app at all is still "not in a call" rather than
	// "unknown" - Unknown is reserved for a detector that failed to run.
	state := model.StateInactive
	if confidence >= activeThreshold {
		state = model.StateActive
	}

	return model.DetectionResult{
		App:        r.App,
		State:      state,
		Confidence: confidence,
		Reasons:    reasons,
		Timestamp:  now,
	}
}

// Evaluate scores every rule in the config against one observation.
func (c *Config) Evaluate(obs Observation, activeThreshold float64, now time.Time) []model.DetectionResult {
	results := make([]model.DetectionResult, 0, len(c.Rules))
	for _, r := range c.Rules {
		results = append(results, r.Evaluate(obs, activeThreshold, now))
	}
	return results
}
