package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nuzar/termdoc/internal/model"
	"github.com/nuzar/termdoc/internal/render"
)

type candidate struct {
	meta  model.Meta
	entry model.Entry
	level int
}

func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "show <name>",
		Aliases: []string{"get", "open", "cat"},
		Short:   "Read an entry",
		Long: `Read a documentation entry.

The name may be prefixed with a docset id, for example:
  termdoc show go:fmt.Println
  termdoc show dash-Go:fmt.Println`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			docset, _ := cmd.Flags().GetString("docset")
			asMD, _ := cmd.Flags().GetBool("md")
			asHTML, _ := cmd.Flags().GetBool("html")
			first, _ := cmd.Flags().GetBool("first")
			return runShow(args[0], docset, asMD, asHTML, first)
		},
	}
	cmd.Flags().StringP("docset", "d", "", "limit lookup to one docset")
	cmd.Flags().Bool("md", false, "print Markdown instead of rendered text")
	cmd.Flags().Bool("html", false, "print the extracted HTML fragment")
	cmd.Flags().Bool("first", false, "show the first match when several exist")
	return cmd
}

func runShow(name, docset string, asMD, asHTML, first bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("empty entry name")
	}
	if docset == "" {
		if i := strings.Index(name, ":"); i > 0 {
			if ms, err := app.Store.Resolve(name[:i]); err == nil && len(ms) == 1 {
				docset = name[:i]
				name = name[i+1:]
			}
		}
	}

	cands, err := findEntries(name, docset)
	if err != nil {
		return err
	}
	if len(cands) == 0 {
		// Fall back to a search: if there is exactly one hit, open it.
		results, serr := searchDocs(docset, name, 10, true)
		if serr == nil && len(results) == 1 {
			r := results[0]
			return renderEntry(r.DocsetID, r.Entry, asMD, asHTML)
		}
		if serr == nil && len(results) > 1 {
			printCandidates(resultsToCandidates(results))
			return fmt.Errorf("no exact entry named %q; %d close matches listed above", name, len(results))
		}
		return fmt.Errorf("no entry named %q (try `termdoc search %s`)", name, name)
	}
	cands = preferExact(cands)
	cands = preferPrimary(cands)
	if len(cands) > 1 && !first {
		printCandidates(cands)
		return fmt.Errorf("entry %q is ambiguous; use --docset or --first", name)
	}
	c := cands[0]
	return renderEntry(c.meta.ID, c.entry, asMD, asHTML)
}

func findEntries(name, docset string) ([]candidate, error) {
	metas, err := app.Store.Resolve(docset)
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return nil, fmt.Errorf("no docsets installed")
	}
	var out []candidate
	for _, m := range metas {
		d, err := app.Store.Load(m.ID)
		if err != nil {
			app.eprintf("warning: %v\n", err)
			continue
		}
		for _, e := range d.FindEntry(name) {
			level := 0
			if !strings.EqualFold(e.Name, name) {
				// Matched only after normalizing a trailing "()".
				level = 1
			}
			out = append(out, candidate{meta: m, entry: e, level: level})
		}
		d.Close()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].level != out[j].level {
			return out[i].level < out[j].level
		}
		ri, rj := typeRank(out[i].entry.Type), typeRank(out[j].entry.Type)
		if ri != rj {
			return ri < rj
		}
		if out[i].meta.ID != out[j].meta.ID {
			return out[i].meta.ID < out[j].meta.ID
		}
		return out[i].entry.Page < out[j].entry.Page
	})
	return out, nil
}

// preferExact drops candidates that only match after normalizing a trailing
// "()", so `show fmt.Println` prefers the entry literally named that.
func preferExact(cands []candidate) []candidate {
	if len(cands) < 2 {
		return cands
	}
	best := cands[0].level
	if best == 0 {
		out := cands[:0]
		for _, c := range cands {
			if c.level == 0 {
				out = append(out, c)
			}
		}
		return out
	}
	return cands
}

func preferPrimary(cands []candidate) []candidate {
	var primary []candidate
	for _, c := range cands {
		if typeRank(c.entry.Type) < 9 {
			primary = append(primary, c)
		}
	}
	if len(primary) > 0 {
		return primary
	}
	return cands
}

func typeRank(t string) int {
	switch strings.ToLower(t) {
	case "", "function", "method", "macro", "command", "operator", "constructor", "instance", "callback":
		return 0
	case "class", "type", "struct", "interface", "enum", "protocol", "typedef", "trait":
		return 1
	case "constant", "variable", "property", "field", "attribute", "event":
		return 2
	case "package", "module", "namespace", "library", "section":
		return 3
	case "sample", "example", "guide", "tutorial":
		return 9
	default:
		return 5
	}
}

func renderEntry(docsetID string, e model.Entry, asMD, asHTML bool) error {
	d, err := app.Store.Load(docsetID)
	if err != nil {
		return err
	}
	defer d.Close()

	if asHTML {
		frag, err := d.EntryHTML(e)
		if err != nil {
			return err
		}
		app.printf("%s\n", frag)
		return nil
	}
	md, err := d.EntryMarkdown(e)
	if err != nil {
		return err
	}
	if asMD {
		app.printf("%s", md)
		return nil
	}
	rendered, err := render.RenderMarkdown(md, app.termOptions())
	if err != nil {
		return err
	}
	render.Page(rendered, app.termOptions())
	return nil
}

func printCandidates(cands []candidate) {
	if len(cands) == 0 {
		return
	}
	nameW, typeW := 4, 4
	for _, c := range cands {
		if n := len(c.entry.Name); n > nameW {
			nameW = n
		}
		if n := len(c.entry.Type); n > typeW {
			typeW = n
		}
	}
	if nameW > 52 {
		nameW = 52
	}
	if typeW > 16 {
		typeW = 16
	}
	for _, c := range cands {
		app.printf("  %s  %s  %s\n",
			app.cyan(pad(truncate(c.entry.Name, nameW), nameW)),
			app.dim(pad(truncate(c.entry.Type, typeW), typeW)),
			app.yellow(c.meta.ID))
	}
}

func resultsToCandidates(rs []model.Result) []candidate {
	out := make([]candidate, 0, len(rs))
	for _, r := range rs {
		out = append(out, candidate{
			meta:  model.Meta{ID: r.DocsetID, Title: r.DocsetTitle},
			entry: r.Entry,
		})
	}
	return out
}
