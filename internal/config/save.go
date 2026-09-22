package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Settings is the subset of the configuration the tray UI can change.
//
// It is deliberately small. Everything here is either a toggle, a pick from
// the current environment, or one of the four broker fields that genuinely
// need typing. Thresholds, debounces and poll intervals stay in the file,
// where changing them is a considered act rather than a stray click.
type Settings struct {
	BrokerHost string
	BrokerPort int
	Username   string
	// Password holds the expanded value so a dialog can pre-fill it. Save
	// only writes it back to the file when PasswordChanged is true — see
	// that field's comment.
	Password        string
	PasswordChanged bool
	Detectors       map[string]bool
	AllowedNetworks []NetworkRule
	Discovery       bool
}

// Settings extracts the UI-editable subset of the configuration.
//
// PasswordChanged starts false: Settings() is also how a mutation that has
// nothing to do with the password (toggling a detector, editing the
// allow-list) gets its starting point, and such a save must leave
// mqtt.password exactly as the file had it — literal or ${VAR} — rather than
// overwrite it with today's expanded value.
func (c *Config) Settings() Settings {
	detectors := make(map[string]bool, len(c.Detectors))
	for name, d := range c.Detectors {
		detectors[name] = d.Enabled
	}

	return Settings{
		BrokerHost:      c.MQTT.Host,
		BrokerPort:      c.MQTT.Port,
		Username:        c.MQTT.Username,
		Password:        c.MQTT.Password,
		PasswordChanged: false,
		Detectors:       detectors,
		AllowedNetworks: c.AllowedNetworks,
		Discovery:       c.MQTT.Discovery.Enabled,
	}
}

// Save writes settings into the YAML file at path, in place.
//
// It edits the existing document rather than re-serialising a struct, because
// the shipped config is mostly comments explaining what each field does. A
// round trip through yaml.Marshal would throw all of that away the first time
// a user toggled a checkbox, leaving them with a file they no longer
// understand. Keys that are absent are appended; keys that are present keep
// their position and their comments.
//
// The file is written atomically and with owner-only permissions, since it now
// holds the broker password.
func Save(path string, s Settings) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	root := documentRoot(&doc)
	if root == nil {
		return fmt.Errorf("config %s is not a YAML mapping", path)
	}

	setScalar(root, "!!str", s.BrokerHost, "mqtt", "host")
	setScalar(root, "!!int", fmt.Sprint(s.BrokerPort), "mqtt", "port")
	setScalar(root, "!!str", s.Username, "mqtt", "username")
	// Only touch mqtt.password when the caller actually changed it. Settings
	// always carries the *expanded* value (so a dialog can show it), and
	// writing that back unconditionally would turn a ${VAR} reference into
	// the literal secret on every unrelated save (e.g. toggling a detector).
	if s.PasswordChanged {
		setScalar(root, "!!str", s.Password, "mqtt", "password")
	}
	setScalar(root, "!!bool", fmt.Sprint(s.Discovery), "mqtt", "discovery", "enabled")

	for name, enabled := range s.Detectors {
		setScalar(root, "!!bool", fmt.Sprint(enabled), "detectors", name, "enabled")
	}

	if err := setSequence(root, s.AllowedNetworks, "allowed_networks"); err != nil {
		return err
	}

	out, err := marshalDocument(&doc)
	if err != nil {
		return err
	}

	// Validate before replacing the file. A UI that can write a config the
	// agent then refuses to load would be worse than no UI at all.
	if _, err := Parse(out); err != nil {
		return fmt.Errorf("refusing to save an invalid config: %w", err)
	}

	return writeAtomic(path, out)
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

// child returns the value node for key within a mapping, creating the key if
// it is absent.
func child(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}

	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapping.Content = append(mapping.Content, keyNode, valNode)
	return valNode
}

// setScalar sets a scalar value at a nested key path, creating intermediate
// mappings as needed. Existing nodes keep their comments and position.
func setScalar(root *yaml.Node, tag, value string, path ...string) {
	node := root
	for _, key := range path[:len(path)-1] {
		node = child(node, key)
		if node.Kind != yaml.MappingNode {
			// A scalar where a mapping is needed means the user hand-edited
			// the file into a shape we cannot extend. Replace it rather than
			// panicking; validation will catch anything genuinely wrong.
			*node = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
	}

	leaf := child(node, path[len(path)-1])
	comment, line, foot := leaf.HeadComment, leaf.LineComment, leaf.FootComment
	*leaf = yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
	leaf.HeadComment, leaf.LineComment, leaf.FootComment = comment, line, foot
}

// setSequence replaces a whole list. Lists are rewritten rather than merged
// because an allow-list is edited as a unit: entries are added and removed,
// and trying to match them up positionally would be guesswork.
func setSequence(root *yaml.Node, value any, path ...string) error {
	var encoded yaml.Node
	if err := encoded.Encode(value); err != nil {
		return fmt.Errorf("encode %v: %w", path, err)
	}
	// An empty list encodes as null, which would read back as "no rules" and
	// deny everything. That is the correct meaning, but write it explicitly.
	if encoded.Kind == 0 || encoded.Tag == "!!null" {
		encoded = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	}

	node := root
	for _, key := range path[:len(path)-1] {
		node = child(node, key)
	}

	leaf := child(node, path[len(path)-1])
	comment, line, foot := leaf.HeadComment, leaf.LineComment, leaf.FootComment
	*leaf = encoded
	leaf.HeadComment, leaf.LineComment, leaf.FootComment = comment, line, foot
	return nil
}

func marshalDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("render config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("render config: %w", err)
	}
	return buf.Bytes(), nil
}

// backupSuffix names the one prior generation writeAtomic keeps, so a save
// that lands mid-write (or a config that turns out unreadable for any other
// reason) still leaves the previous, known-good file one rename away. Load's
// error mentions it by this exact name.
const backupSuffix = ".bak"

// writeAtomic writes via a temporary file in the same directory, then renames
// it over path.
//
// The rename makes the replace atomic against a crash of this process: at no
// point does path point at a half-written file. tmp.Sync() before the rename
// additionally makes it durable against power loss or a hard reset — without
// it, NTFS can journal the rename before the temp file's data has actually
// reached disk, so a power cut in that window could leave path empty even
// though the process itself was never interrupted. This is not a full
// guarantee against every kind of storage failure (the rename itself is not
// fsync'd, and neither is the directory entry it produces), but it closes the
// gap that mattered here.
//
// Before the rename, any existing file at path is kept as path+backupSuffix,
// so one prior generation always survives a bad write.
func writeAtomic(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".callmqtt-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()

	defer func() {
		// Harmless if the rename already succeeded.
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("flush temporary config: %w", err)
	}
	// Owner-only, because the file now holds a plaintext broker password.
	//
	// On Windows this is close to a no-op: Chmod there only toggles the
	// read-only attribute, and Unix mode bits are not modelled. Protection on
	// Windows comes instead from the default location, inside the user's
	// %AppData%, which other standard users cannot read. On macOS the mode is
	// real and does the work.
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("restrict config permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}

	// Best-effort and silent: the common failure is that path does not exist
	// yet (the very first save), and any other failure to back up must not
	// block replacing the config either, since the new file is already
	// durably written and is strictly better than what is there now.
	_ = os.Rename(path, path+backupSuffix)

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config %s: %w", path, err)
	}
	return nil
}
