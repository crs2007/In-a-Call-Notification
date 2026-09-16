// validate_readme checks a README against the readme-standards checklist.
//
// Usage:
//
//	go run ./.claude/skills/readme-standards/scripts/validate_readme.go [--strict] [path/to/README.md]
//
// Exit status is 1 when a required section is missing, or when --strict is
// given and a recommended section is missing. Standard library only.
package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type check struct {
	name     string
	required bool
	ok       bool
	hint     string
}

// readme is the parsed shape of the document the checks operate on.
type readme struct {
	lines    []string
	headings []string // heading text, lowercased, without the leading #s
	words    int
	body     string
}

func main() {
	strict := false
	path := "README.md"
	for _, arg := range os.Args[1:] {
		switch {
		case arg == "--strict":
			strict = true
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag %q\n", arg)
			os.Exit(2)
		default:
			path = arg
		}
	}

	doc, err := load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	checks := run(doc)

	failedRequired, failedRecommended := 0, 0
	fmt.Printf("README check: %s (%d words)\n\n", path, doc.words)
	printGroup("Required", checks, true, &failedRequired)
	printGroup("Recommended", checks, false, &failedRecommended)

	fmt.Println()
	switch {
	case failedRequired > 0:
		fmt.Printf("FAIL: %d required section(s) missing\n", failedRequired)
		os.Exit(1)
	case strict && failedRecommended > 0:
		fmt.Printf("FAIL (strict): %d recommended section(s) missing\n", failedRecommended)
		os.Exit(1)
	case failedRecommended > 0:
		fmt.Printf("PASS with %d recommendation(s)\n", failedRecommended)
	default:
		fmt.Println("PASS")
	}
}

func printGroup(title string, checks []check, required bool, failed *int) {
	fmt.Printf("%s:\n", title)
	for _, c := range checks {
		if c.required != required {
			continue
		}
		mark := "[x]"
		if !c.ok {
			mark = "[ ]"
			*failed++
		}
		fmt.Printf("  %s %s", mark, c.name)
		if !c.ok && c.hint != "" {
			fmt.Printf("  — %s", c.hint)
		}
		fmt.Println()
	}
}

func load(path string) (*readme, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	doc := &readme{}
	var sb strings.Builder
	headingRe := regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	inFence := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		doc.lines = append(doc.lines, line)
		sb.WriteString(line)
		sb.WriteByte('\n')
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			doc.headings = append(doc.headings, strings.ToLower(strings.TrimSpace(m[2])))
		}
		doc.words += len(strings.Fields(line))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	doc.body = sb.String()
	return doc, nil
}

func run(d *readme) []check {
	return []check{
		{
			name:     "Project title (H1 on the first line)",
			required: true,
			ok:       d.hasTitle(),
			hint:     "start the file with `# Project Name`",
		},
		{
			name:     "Badges under the title",
			required: true,
			ok:       d.hasBadge(),
			hint:     "add at least one shields.io / status badge in the first 15 lines",
		},
		{
			name:     "Elevator pitch before the first ## heading",
			required: true,
			ok:       d.hasPitch(),
			hint:     "1–2 plain sentences right after the title/badges",
		},
		{
			name:     "Visual demonstration (image, GIF, or fenced code block)",
			required: true,
			ok:       d.hasVisual(),
			hint:     "add a screenshot/GIF or a ```fenced``` example of it in action",
		},
		{
			name:     "Installation section",
			required: true,
			ok:       d.hasHeading(`^(install|installation|getting started|setup)\b`),
			hint:     "add `## Installation` with copy-pasteable steps",
		},
		{
			name:     "Quick Start / Usage section",
			required: true,
			ok:       d.hasHeading(`^(quick ?start|usage|how to use|getting started)\b`),
			hint:     "add `## Quick Start` with the minimum to get a result",
		},
		{
			name:     "License section",
			required: true,
			ok:       d.hasHeading(`^licen[cs]e\b`),
			hint:     "add `## License` naming the license and linking LICENSE",
		},
		{
			name:     "Table of Contents (only needed over 1,000 words)",
			required: false,
			ok:       d.words <= 1000 || d.hasHeading(`^(table of )?contents\b`),
			hint:     "README exceeds 1,000 words; add a `## Table of Contents`",
		},
		{
			name:     "Configuration / API section",
			required: false,
			ok:       d.hasHeading(`\b(config|configuration|api|options|settings|reference)\b`),
			hint:     "document every flag, config key or endpoint with its default",
		},
		{
			name:     "Contributing section or link to CONTRIBUTING.md",
			required: false,
			ok:       d.hasHeading(`\bcontribut`) || strings.Contains(d.body, "CONTRIBUTING.md"),
			hint:     "add `## Contributing` or link CONTRIBUTING.md",
		},
		{
			name:     "FAQ / Troubleshooting section",
			required: false,
			ok:       d.hasHeading(`\b(faq|troubleshoot|known issues)`),
			hint:     "add `## FAQ / Troubleshooting` with the common pitfalls",
		},
	}
}

func (d *readme) hasTitle() bool {
	for _, l := range d.lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		return regexp.MustCompile(`^#\s+\S`).MatchString(l)
	}
	return false
}

var badgeRe = regexp.MustCompile(`\[!\[[^\]]*\]\([^)]*\)\]\([^)]*\)|!\[[^\]]*\]\([^)]*(shields\.io|badge)[^)]*\)`)

func (d *readme) hasBadge() bool {
	limit := 15
	if len(d.lines) < limit {
		limit = len(d.lines)
	}
	return badgeRe.MatchString(strings.Join(d.lines[:limit], "\n"))
}

// hasPitch looks for a prose paragraph between the H1 and the first H2.
func (d *readme) hasPitch() bool {
	seenTitle := false
	for _, l := range d.lines {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "# "):
			seenTitle = true
			continue
		case strings.HasPrefix(t, "## "):
			return false
		case !seenTitle, badgeRe.MatchString(t), strings.HasPrefix(t, "!["), strings.HasPrefix(t, "```"):
			continue
		}
		// A blockquote pitch (`> ...`) counts too; only require some letters.
		return regexp.MustCompile(`[A-Za-z]{3,}`).MatchString(strings.TrimLeft(t, "> "))
	}
	return false
}

var (
	imageRe = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)|<img\s`)
	mediaRe = regexp.MustCompile(`(?i)\.(gif|png|jpe?g|webp|svg|mp4)\b`)
)

func (d *readme) hasVisual() bool {
	for _, l := range d.lines {
		if badgeRe.MatchString(l) {
			// A badge is an image, but not a demonstration.
			l = badgeRe.ReplaceAllString(l, "")
		}
		if imageRe.MatchString(l) || mediaRe.MatchString(l) {
			return true
		}
	}
	return strings.Count(d.body, "```") >= 2
}

func (d *readme) hasHeading(pattern string) bool {
	re := regexp.MustCompile(pattern)
	for _, h := range d.headings {
		if re.MatchString(h) {
			return true
		}
	}
	return false
}
